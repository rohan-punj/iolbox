#!/usr/bin/env bash
# P0 acceptance gate from docs/learning-tools-nodes-plan.md (T0.1--T0.9).
# Run on a real Linux target as root: sudo bash docs/tests/p0-spike.sh
set -Eeuo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BUILD_ROOT="${IOLBOX_P0_BUILD_DIR:-$REPO_ROOT/docs/tests/.p0-build}"
BIN_ROOT="$BUILD_ROOT/bin"
LOG_ROOT="$BUILD_ROOT/log"
mkdir -p "$BIN_ROOT" "$LOG_ROOT"

if [[ "$(id -u)" != 0 ]]; then
	echo "FAIL: docs/tests/p0-spike.sh must run as root (sudo bash ...)" >&2
	exit 1
fi

pass() { echo "PASS $*"; }
fail() { echo "FAIL $*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || fail "missing required command: $1"; }

for command_name in go ip bash stat curl capsh tcpdump python3; do need "$command_name"; done
id ioltool >/dev/null 2>&1 || fail "missing required unprivileged account: ioltool"
python3 -c 'from scapy.all import ARP, Ether, sendp' >/dev/null 2>&1 || \
	fail "python3 scapy is required for T0.9"

echo "==> building standalone P0 helpers"
for module in tool-stubgui tool-hostile p0-launcher p0-reaper p0-stale; do
	(
		cd "$REPO_ROOT/tools/$module"
		GOOS=linux GOARCH=amd64 go build -trimpath -o "$BIN_ROOT/$module" .
	)
done

STUB="$BIN_ROOT/tool-stubgui"
HOSTILE="$BIN_ROOT/tool-hostile"
NATIVE_LAUNCHER="$BIN_ROOT/p0-launcher"
REAPER="$BIN_ROOT/p0-reaper"
STALE_REAPER="$BIN_ROOT/p0-stale"
ID="$(( $$ % 1000000 ))"
TOOL_NS="iolt$ID"
TOOL_VETH="vtool$ID"
TOOL_TMP="p0x$ID"
HOST_IFACE="p0host$ID"

CGROUP_REL="$(awk -F: '$1 == 0 { print $3; exit }' /proc/self/cgroup)"
[[ "$CGROUP_REL" != "/" ]] || fail "current process is in the cgroup filesystem root"
CGROUP_BASE="/sys/fs/cgroup$CGROUP_REL"
[[ -d "$CGROUP_BASE" && "$CGROUP_BASE" != "/sys/fs/cgroup" ]] || \
	fail "no non-root delegated cgroup found from /proc/self/cgroup: $CGROUP_BASE"
CONTROLLERS="$(cat "$CGROUP_BASE/cgroup.controllers")"
for controller in memory pids cpu; do
	grep -Eq "(^|[[:space:]])$controller($|[[:space:]])" <<<"$CONTROLLERS" || \
		fail "delegated cgroup lacks controller: $controller"
done

RUN_PARENT=/run/iolbox/tool
STATE_PARENT="${IOLBOX_P0_STATE_DIR:-/var/lib/iolbox/p0-spike-$ID}"
mkdir -p "$RUN_PARENT" "$STATE_PARENT"
chown root:root "$RUN_PARENT" "$STATE_PARENT"
chmod 0755 "$RUN_PARENT" "$STATE_PARENT"

declare -a CAGES=() NETNS=() DEVS=() RUNDIRS=() PIDS=()
CGROUP_D=""
SUPERVISOR_LEAF=""
CGROUP_D_SUBTREE_ENABLED=0
BRIDGE_CREATED=0

safe_remove_dir() {
	local path="$1"
	[[ -n "$path" && ( "$path" == "$STATE_PARENT" || "$path" == "$RUN_PARENT"/* || "$path" == "$STATE_PARENT"/* || "$path" == "$BUILD_ROOT"/* ) ]] || return 0
	rm -rf -- "$path"
}

kill_cgroup() {
	local cg="$1"
	[[ -d "$cg" ]] || return 0
	echo 1 > "$cg/cgroup.kill" 2>/dev/null || true
	for _ in $(seq 1 200); do
		if grep -Eq '^populated[[:space:]]+0$' "$cg/cgroup.events" 2>/dev/null; then
			rmdir "$cg" 2>/dev/null || true
			return 0
		fi
		sleep 0.05
	done
	echo "WARN: cgroup remained populated during cleanup: $cg" >&2
}

cleanup() {
	set +e
	for pid in "${PIDS[@]-}"; do
		[[ -n "$pid" ]] && kill -KILL "$pid" 2>/dev/null || true
	done
	for cg in "${CAGES[@]-}"; do
		[[ -n "$cg" ]] && kill_cgroup "$cg"
	done
	# Undo the T0.3 bootstrap in reverse: a cgroup with controllers enabled in
	# cgroup.subtree_control may not hold processes, so <D> has to be emptied of
	# enabled controllers before this PID can migrate back up into it.
	if [[ "$CGROUP_D_SUBTREE_ENABLED" == 1 ]]; then
		echo '-memory -pids -cpu' > "$CGROUP_BASE/cgroup.subtree_control" 2>/dev/null || true
		CGROUP_D_SUBTREE_ENABLED=0
	fi
	if [[ -n "$SUPERVISOR_LEAF" ]]; then
		echo "$$" > "$CGROUP_BASE/cgroup.procs" 2>/dev/null || true
		rmdir "$SUPERVISOR_LEAF" 2>/dev/null || true
	fi
	for ns in "${NETNS[@]-}"; do
		[[ -n "$ns" ]] && ip netns del "$ns" 2>/dev/null || true
	done
	for dev in "${DEVS[@]-}"; do
		[[ -n "$dev" ]] && ip link del dev "$dev" 2>/dev/null || true
	done
	if [[ "$BRIDGE_CREATED" == 1 ]]; then ip link del dev iolbr0 2>/dev/null || true; fi
	for dir in "${RUNDIRS[@]-}"; do
		[[ -n "$dir" ]] && safe_remove_dir "$dir"
	done
	safe_remove_dir "$STATE_PARENT"
}
trap cleanup EXIT

make_cage() {
	local name="$1" memory="$2" pids="$3"
	local cg="$CGROUP_D/$name"
	if ! mkdir "$cg"; then
		echo "DIAG: mkdir failed for cage $cg; parent listing follows:" >&2
		ls -la "$(dirname "$cg")" >&2 || true
		fail "make_cage could not create $cg"
	fi
	if [[ ! -w "$cg/cgroup.procs" ]]; then
		echo "DIAG: $cg exists but has no writable cgroup.procs; listing follows:" >&2
		ls -la "$cg" >&2 || true
		fail "make_cage created $cg without a usable cgroup.procs"
	fi
	echo "$memory" > "$cg/memory.max"
	echo 0 > "$cg/memory.swap.max"
	echo "$pids" > "$cg/pids.max"
	echo 100 > "$cg/cpu.weight"
	printf '%s\n' "$cg"
}

wait_for_file() {
	local path="$1"
	for _ in $(seq 1 200); do
		[[ -e "$path" ]] && return 0
		sleep 0.05
	done
	fail "timed out waiting for $path"
}

wait_for_file_optional() {
	local path="$1"
	for _ in $(seq 1 200); do
		[[ -e "$path" ]] && return 0
		sleep 0.05
	done
	return 1
}

# A freshly created link is still being mutated by udev when `ip link add`
# returns: systemd-udevd's stock /usr/lib/systemd/network/99-default.link sets
# MACAddressPolicy=persistent, so the kernel-random MAC of a brand-new device is
# rewritten roughly 0.3s later. Anything that snapshots the device before that
# settles is racing udev, not observing the code under test.
settle_link() {
	local dev="$1" previous="" current=""
	if command -v udevadm >/dev/null 2>&1; then
		udevadm settle --timeout=5 >/dev/null 2>&1 || true
	fi
	# Also poll the device itself: udevadm may be absent, and `settle` only
	# drains the queue it can see. Two consecutive identical reads 300ms apart
	# is the actual property we need.
	for _ in $(seq 1 20); do
		current="$(ip -o link show dev "$dev" 2>/dev/null || true)"
		[[ -n "$current" && "$current" == "$previous" ]] && return 0
		previous="$current"
		sleep 0.3
	done
	echo "WARN: $dev did not stop changing; baseline may still race udev" >&2
	return 0
}

wait_for_line() {
	local path="$1" pattern="$2"
	for _ in $(seq 1 200); do
		grep -Eq "$pattern" "$path" 2>/dev/null && return 0
		sleep 0.05
	done
	fail "timed out waiting for $pattern in $path"
}

wait_gone() {
	local pid="$1"
	for _ in $(seq 1 200); do
		kill -0 "$pid" 2>/dev/null || return 0
		sleep 0.05
	done
	fail "pid $pid survived cleanup"
}

start_cgroup_command() {
	local cg="$1" log="$2"
	shift 2
	(
		echo "$BASHPID" > "$cg/cgroup.procs"
		exec "$@"
	) >"$log" 2>&1 &
	LAST_PID=$!
	PIDS+=("$LAST_PID")
}

SETPRIV="$(command -v setpriv 2>/dev/null || true)"
LAUNCH_MODE=native
if [[ -n "$SETPRIV" ]]; then
	SETPRIV_VERSION="$("$SETPRIV" --version | sed -nE 's/.* ([0-9]+\.[0-9]+).*/\1/p' | head -n1)"
	if [[ -n "$SETPRIV_VERSION" && "$(printf '%s\n' "$SETPRIV_VERSION" 2.33 | sort -V | head -n1)" == 2.33 ]]; then LAUNCH_MODE=setpriv; fi
fi

start_tool() {
	local ns="$1" cg="$2" target="$3" log="$4"
	(
		# Join the cage HERE, in the root mount namespace, before handing off to
		# `ip netns exec`. iproute2's netns_switch() unshares a mount namespace,
		# makes / a slave, then umount2("/sys", MNT_DETACH) and mounts a fresh
		# per-netns sysfs -- which detaches the cgroup2 mount along with it. Inside
		# `ip netns exec` /sys/fs/cgroup is therefore an EMPTY kernel mount-point
		# stub, so writing "$cg/cgroup.procs" from the inner shell fails ENOENT
		# ("No such file or directory") even though make_cage's mkdir succeeded a
		# moment earlier in this namespace. cgroup membership is inherited across
		# setns(2) and execve(2), so joining before the switch is equivalent and,
		# unlike the inner write, actually observable. start_cgroup_command() has
		# always done it this way, which is why the non-netns T0.3 cages worked.
		if ! echo "$BASHPID" > "$cg/cgroup.procs"; then
			echo "DIAG: could not join cage $cg from the root mount namespace" >&2
			ls -la "$cg" >&2 || true
			exit 1
		fi
		exec ip netns exec "$ns" env -i PATH=/usr/bin:/bin \
			IOLBOX_CGROUP_PATH="$cg" IOLBOX_TARGET="$target" IOLBOX_LAUNCH_MODE="$LAUNCH_MODE" \
			IOLBOX_SETPRIV="$SETPRIV" IOLBOX_NATIVE="$NATIVE_LAUNCHER" \
			IOLBOX_TOOL_SOCK="${IOLBOX_TOOL_SOCK-}" IOLBOX_TOOL_OPTIONS="${IOLBOX_TOOL_OPTIONS-}" \
			IOLBOX_TOOL_STATUS_FILE="${IOLBOX_TOOL_STATUS_FILE-}" IOLBOX_TOOL_IFACE="${IOLBOX_TOOL_IFACE-eth1}" \
			IOLBOX_STUB_GRANDCHILD_PID_FILE="${IOLBOX_STUB_GRANDCHILD_PID_FILE-}" \
			IOLBOX_HOSTILE_ORPHAN_PID_FILE="${IOLBOX_HOSTILE_ORPHAN_PID_FILE-}" \
			IOLBOX_HOSTILE_LINGER="${IOLBOX_HOSTILE_LINGER-}" IOLBOX_HOST_IFACE="${IOLBOX_HOST_IFACE-}" \
			IOLBOX_HOST_FILE="${IOLBOX_HOST_FILE-}" \
			bash -c 'if [[ "$IOLBOX_LAUNCH_MODE" == setpriv ]]; then exec "$IOLBOX_SETPRIV" --reuid ioltool --regid ioltool --clear-groups --no-new-privs --bounding-set -all,+cap_net_raw --inh-caps -all,+cap_net_raw --ambient-caps -all,+cap_net_raw -- "$IOLBOX_TARGET"; else exec "$IOLBOX_NATIVE" --user ioltool -- "$IOLBOX_TARGET"; fi'
	) >"$log" 2>&1 &
	LAST_PID=$!
	PIDS+=("$LAST_PID")
}

echo "==> T0.3 fixture: delegated cgroup 3-level hierarchy"
# Level 1 (<D>) is this process's OWN delegated cgroup -- NOT a probe-created
# child of it. Plan T1.12 is explicit: "a probe that enabled controllers on a
# different, empty parent would not prove the real <D> accepts subtree_control".
# It also matters mechanically: a freshly-created child of <D> inherits an EMPTY
# cgroup.controllers until <D> lists the controllers in its own
# cgroup.subtree_control, so writing "+memory +pids +cpu" to that child fails
# ENOENT (controller not available at this level) instead of the EBUSY the
# ordering guard is asserting. <D> itself already lists memory/pids/cpu in
# cgroup.controllers (checked above -- systemd's Delegate=yes + *Accounting=yes
# propagate them down) and already holds this script's PID as a direct member,
# which is exactly the production supervisor's situation at startup.
#
# The migrate-then-enable bootstrap below is therefore not spike invention: it
# mirrors, for this standalone probe, what T1.4/T1.11 (P1 -- iolbox-supervisor
# startup, NOT yet built) will do on the real <D>. Do not "simplify" it away.
CGROUP_D="$CGROUP_BASE"
SUPERVISOR_LEAF="$CGROUP_D/supervisor"
grep -Eqx "$$" "$CGROUP_D/cgroup.procs" || \
	fail "T0.3 probe PID $$ is not a direct member of the delegated root <D>=$CGROUP_D"
[[ ! -e "$SUPERVISOR_LEAF" ]] || fail "T0.3 refuses to reuse an existing $SUPERVISOR_LEAF"
if python3 - "$CGROUP_D/cgroup.subtree_control" <<'PY'
import errno
import sys

try:
    with open(sys.argv[1], "w", encoding="ascii") as stream:
        stream.write("+memory +pids +cpu\n")
except OSError as error:
    print(f"subtree_control errno={error.errno}")
    raise SystemExit(0 if error.errno == errno.EBUSY else 2)
raise SystemExit(1)
PY
then
	pass "T0.3 ordering guard: populated D returned EBUSY"
else
	fail "T0.3 ordering guard did not return EBUSY while D held the probe PID"
fi
# T1.11 startup order, steps (1)-(3): create the level-2 supervisor leaf,
# migrate our own PID into it so <D> is process-empty, and only THEN enable the
# controllers for <D>'s children. Step (3) before (2) is the EBUSY just proven.
mkdir "$SUPERVISOR_LEAF" || fail "T0.3 could not create the level-2 supervisor leaf $SUPERVISOR_LEAF"
echo "$$" > "$SUPERVISOR_LEAF/cgroup.procs"
[[ ! -s "$CGROUP_D/cgroup.procs" ]] || fail "T0.3 supervisor PID was not migrated out of D"
echo '+memory +pids +cpu' > "$CGROUP_D/cgroup.subtree_control"
CGROUP_D_SUBTREE_ENABLED=1
pass "T0.3 3-level hierarchy: D -> supervisor -> tool leaves"

echo "==> T0.4 fixture: veth create-temp-move-rename"
if ip link show dev eth1 >/dev/null 2>&1; then
	ROOT_ETH1_CREATED=0
else
	ip link add eth1 type dummy
	ROOT_ETH1_CREATED=1
	DEVS+=(eth1)
	# The baseline must be taken only once udev has finished applying its
	# MACAddressPolicy to this brand-new dummy. Capturing it immediately after
	# `ip link add` put the ~0.3s MAC rewrite inside the window covered by the
	# netns/veth work below, so the final "root eth1 untouched" comparison
	# reported a difference this script never caused. The real-eth1 branch above
	# needs no settle: a NIC that already existed is not re-addressed at runtime.
	settle_link eth1
fi
ROOT_ETH1_BEFORE="$(ip -o link show dev eth1)"
ip netns add "$TOOL_NS"
NETNS+=("$TOOL_NS")
ip link add "$TOOL_VETH" type veth peer name "$TOOL_TMP"
DEVS+=("$TOOL_VETH")
ip link set "$TOOL_TMP" netns "$TOOL_NS"
ip netns exec "$TOOL_NS" ip link set "$TOOL_TMP" name eth1
ip netns exec "$TOOL_NS" ip link set lo up
ip netns exec "$TOOL_NS" ip link set eth1 up
ip link set "$TOOL_VETH" up
ip link show dev "$TOOL_VETH" >/dev/null
ip netns exec "$TOOL_NS" ip link show dev eth1 >/dev/null
[[ "$(ip -o link show dev eth1)" == "$ROOT_ETH1_BEFORE" ]] || fail "T0.4 changed root-namespace eth1"
pass "T0.4 temp veth moved then renamed inside $TOOL_NS; root eth1 untouched"

echo "==> T0.1/T0.2/T0.5 fixture: stub GUI, cap transition, AF_UNIX permissions"
TOOL_DIR="$RUN_PARENT/$ID"
install -d -o ioltool -g ioltool -m 0700 "$TOOL_DIR"
RUNDIRS+=("$TOOL_DIR")
IOLBOX_TOOL_SOCK="$TOOL_DIR/gui.sock"
IOLBOX_TOOL_OPTIONS="$TOOL_DIR/options.json"
IOLBOX_TOOL_STATUS_FILE="$TOOL_DIR/status.txt"
IOLBOX_TOOL_IFACE=eth1
printf '%s\n' '{"probe":"p0"}' > "$IOLBOX_TOOL_OPTIONS"
chown ioltool:ioltool "$IOLBOX_TOOL_OPTIONS"
chmod 0600 "$IOLBOX_TOOL_OPTIONS"

CAP_CG="$(make_cage "tool-cap-$ID" 64M 64)"
CAGES+=("$CAP_CG")
STUB_LOG="$LOG_ROOT/stub.log"
start_tool "$TOOL_NS" "$CAP_CG" "$STUB" "$STUB_LOG"
STUB_PID="$LAST_PID"
if ! wait_for_file_optional "$IOLBOX_TOOL_STATUS_FILE" || ! grep -Eq '^Cap(Eff|Prm|Inh|Amb|Bnd):[[:space:]]+0*2000$' "$IOLBOX_TOOL_STATUS_FILE"; then
	if [[ "$LAUNCH_MODE" == setpriv ]]; then
		echo "INFO: pinned setpriv failed the raw-only status gate; retrying native fallback"
		kill -KILL "$STUB_PID" 2>/dev/null || true
		kill_cgroup "$CAP_CG"
		CAP_CG="$(make_cage "tool-cap-native-$ID" 64M 64)"
		CAGES+=("$CAP_CG")
		LAUNCH_MODE=native
		rm -f "$IOLBOX_TOOL_STATUS_FILE" "$IOLBOX_TOOL_SOCK"
		start_tool "$TOOL_NS" "$CAP_CG" "$STUB" "$STUB_LOG"
		STUB_PID="$LAST_PID"
		wait_for_file "$IOLBOX_TOOL_STATUS_FILE"
	fi
fi
grep -Eq '^Cap(Eff|Prm|Inh|Amb|Bnd):[[:space:]]+0*2000$' "$IOLBOX_TOOL_STATUS_FILE" || fail "T0.2 final CapEff/Prm/Inh/Amb/Bnd is not raw-only"
grep -Eq '^NoNewPrivs:[[:space:]]+1$' "$IOLBOX_TOOL_STATUS_FILE" || fail "T0.2 final process lacks no-new-privs"
pass "T0.1 stub GUI bound AF_UNIX and served its launch target"
pass "T0.2 final /proc/self/status has raw-only caps and no-new-privs (mode=$LAUNCH_MODE)"

CAPSH="$(command -v capsh)"
CAPSH_LOG="$LOG_ROOT/capsh.log"
start_tool "$TOOL_NS" "$CAP_CG" "$CAPSH" "$CAPSH_LOG"
CAPSH_PID="$LAST_PID"
set +e
wait "$CAPSH_PID"
CAPSH_RC=$?
set -e
[[ "$CAPSH_RC" == 0 ]] || fail "capsh --print failed"
grep -Eq 'Bounding set[[:space:]]*=.*cap_net_raw' "$CAPSH_LOG" || fail "capsh did not show raw-only bounding set"
! grep -Eq 'Bounding set[[:space:]]*=.*cap_net_admin' "$CAPSH_LOG" || fail "capsh still shows NET_ADMIN in bounding set"
grep -Eq 'no-new-privs=1' "$CAPSH_LOG" || fail "capsh did not show no-new-privs"
if grep -Eq 'secure-noroot:[[:space:]]+yes.*locked|secure-keep-caps:[[:space:]]+yes.*locked' "$CAPSH_LOG"; then
	pass "T0.2 capability transition includes securebits locks"
else
	if [[ "$LAUNCH_MODE" == setpriv ]]; then
		echo "INFO: setpriv lacks required securebits locks; native fallback selected"
		kill -KILL "$STUB_PID" 2>/dev/null || true
		kill_cgroup "$CAP_CG"
		CAP_CG="$(make_cage "tool-cap-securebits-$ID" 64M 64)"
		CAGES+=("$CAP_CG")
		LAUNCH_MODE=native
		rm -f "$IOLBOX_TOOL_STATUS_FILE" "$IOLBOX_TOOL_SOCK"
		start_tool "$TOOL_NS" "$CAP_CG" "$STUB" "$STUB_LOG"
		STUB_PID="$LAST_PID"
		wait_for_file "$IOLBOX_TOOL_STATUS_FILE"
		grep -Eq '^Cap(Eff|Prm|Inh|Amb|Bnd):[[:space:]]+0*2000$' "$IOLBOX_TOOL_STATUS_FILE" || fail "native fallback final caps are not raw-only"
		CAPSH_NATIVE_LOG="$LOG_ROOT/capsh-native.log"
		start_tool "$TOOL_NS" "$CAP_CG" "$CAPSH" "$CAPSH_NATIVE_LOG"
		CAPSH_NATIVE_PID="$LAST_PID"
		set +e
		wait "$CAPSH_NATIVE_PID"
		CAPSH_NATIVE_RC=$?
		set -e
		[[ "$CAPSH_NATIVE_RC" == 0 ]] || fail "native fallback capsh --print failed"
		grep -Eq 'secure-noroot:[[:space:]]+yes.*locked|secure-keep-caps:[[:space:]]+yes.*locked' "$CAPSH_NATIVE_LOG" || fail "native fallback did not set securebits locks"
	else
		fail "native capability launcher did not set securebits locks"
	fi
fi

curl --fail --silent --show-error --unix-socket "$IOLBOX_TOOL_SOCK" http://localhost/healthz | grep -qx 'ok' || fail "T0.5 root namespace could not dial AF_UNIX GUI"
curl --fail --silent --show-error --unix-socket "$IOLBOX_TOOL_SOCK" http://localhost/ | grep -qx 'iolbox tool stub gui' || fail "T0.1 root path failed"
grep -qx 'stubgui-started' <(tail -n1 "$IOLBOX_TOOL_OPTIONS") || fail "T0.5 stub could not read/write options.json"
[[ "$(stat -c '%U:%G %a' "$TOOL_DIR")" == 'ioltool:ioltool 700' ]] || fail "T0.5 socket directory is not ioltool 0700"
[[ "$(stat -c '%U:%G %a' "$IOLBOX_TOOL_OPTIONS")" == 'ioltool:ioltool 600' ]] || fail "T0.5 options file is not ioltool 0600"
pass "T0.5 root namespace dialed AF_UNIX; ioltool read/wrote 0600 options"
kill -TERM "$STUB_PID" 2>/dev/null || true
kill_cgroup "$CAP_CG"

MEM_CG="$(make_cage "tool-memory-$ID" 32M 64)"
CAGES+=("$MEM_CG")
MEM_LOG="$LOG_ROOT/memory.log"
start_cgroup_command "$MEM_CG" "$MEM_LOG" "$HOSTILE" --memory-hog
MEM_PID="$LAST_PID"
wait_gone "$MEM_PID"
grep -Eq '^oom_kill[[:space:]]+[1-9][0-9]*$' "$MEM_CG/memory.events" || fail "T0.3 memory.max did not OOM-kill hog"
pass "T0.3 level-3 memory.max OOM-killed the memory hog"
kill_cgroup "$MEM_CG"

FORK_CG="$(make_cage "tool-pids-$ID" 64M 16)"
CAGES+=("$FORK_CG")
FORK_LOG="$LOG_ROOT/fork.log"
start_cgroup_command "$FORK_CG" "$FORK_LOG" "$HOSTILE" --fork-bomb
FORK_PID="$LAST_PID"
wait_for_line "$FORK_LOG" '^FORK_BOUNDED '
CURRENT_PIDS="$(cat "$FORK_CG/pids.current")"
MAX_PIDS="$(cat "$FORK_CG/pids.max")"
(( CURRENT_PIDS <= MAX_PIDS )) || fail "T0.3 pids.max exceeded: $CURRENT_PIDS > $MAX_PIDS"
pass "T0.3 level-3 pids.max bounded fork bomb at $CURRENT_PIDS/$MAX_PIDS"
kill_cgroup "$FORK_CG"

# T0.6: p0-reaper sets PR_SET_CHILD_SUBREAPER, peeks with WNOWAIT, leaves the
# registered direct child to exec.Cmd.Wait, and reaps only the orphan.
REAPER_DIR="$RUN_PARENT/reaper-$ID"
install -d -o ioltool -g ioltool -m 0700 "$REAPER_DIR"
RUNDIRS+=("$REAPER_DIR")
IOLBOX_TOOL_SOCK="$REAPER_DIR/gui.sock"
IOLBOX_TOOL_OPTIONS="$REAPER_DIR/options.json"
IOLBOX_STUB_GRANDCHILD_PID_FILE="$REAPER_DIR/grandchild.pid"
printf '%s\n' '{}' > "$IOLBOX_TOOL_OPTIONS"
chown ioltool:ioltool "$IOLBOX_TOOL_OPTIONS"
REAPER_CG="$(make_cage "tool-reaper-$ID" 64M 64)"
CAGES+=("$REAPER_CG")
REAPER_RESULT="$REAPER_DIR/result.txt"
(
	echo "$BASHPID" > "$REAPER_CG/cgroup.procs"
	export IOLBOX_TOOL_SOCK IOLBOX_TOOL_OPTIONS IOLBOX_STUB_GRANDCHILD_PID_FILE
	exec "$REAPER" --target "$STUB" --result "$REAPER_RESULT" --setpriv "$SETPRIV"
) >"$LOG_ROOT/reaper.log" 2>&1 &
REAPER_PID=$!
PIDS+=("$REAPER_PID")
wait_for_file "$REAPER_RESULT"
wait "$REAPER_PID"
grep -Eq '^SUBREAPER PASS$' "$REAPER_RESULT" || fail "T0.6 subreaper was not enabled"
grep -Eq '^DIRECT_CHILD_WAIT PASS$' "$REAPER_RESULT" || fail "T0.6 direct status was stolen from exec.Cmd.Wait"
grep -Eq '^ORPHAN_REAP PASS$' "$REAPER_RESULT" || fail "T0.6 orphan was not reaped by WNOWAIT ownership split"
pass "T0.6 registered GUI Wait and orphan reaping are separate"
kill_cgroup "$REAPER_CG"

# T0.7: make a durable instance-id + state file, create real leaked objects in
# a child, SIGKILL that child, then use a fresh p0-stale invocation to sweep.
INSTANCE_FILE="$STATE_PARENT/instance-id"
STATE_DIR="$STATE_PARENT/state"
STALE_RUN_ROOT="$STATE_PARENT/run"
STALE_RUN_DIR="$STALE_RUN_ROOT/node-$ID"
mkdir -p "$STATE_DIR" "$STALE_RUN_DIR"
printf 'p0-install-%s\n' "$ID" > "$INSTANCE_FILE"
INSTANCE_ID="$(cat "$INSTANCE_FILE")"
STALE_NS="iolt$((ID + 1))"
STALE_VETH="vtool$((ID + 1))"
STALE_TMP="p0y$((ID + 1))"
STALE_CG="$CGROUP_D/tool-stale-$ID"
mkdir "$STALE_CG"
echo 64M > "$STALE_CG/memory.max"
echo 0 > "$STALE_CG/memory.swap.max"
echo 64 > "$STALE_CG/pids.max"
echo 100 > "$STALE_CG/cpu.weight"
CAGES+=("$STALE_CG")
cat > "$STATE_DIR/$INSTANCE_ID.json" <<EOF
{"instance_id":"$INSTANCE_ID","cgroup_path":"$STALE_CG","netns":"$STALE_NS","veth":"$STALE_VETH","run_dir":"$STALE_RUN_DIR"}
EOF
READY="$STATE_PARENT/ready"
STALE_PID_FILE="$STALE_RUN_DIR/hostile.pid"
(
	ip netns add "$STALE_NS"
	ip link add "$STALE_VETH" type veth peer name "$STALE_TMP"
	ip link set "$STALE_TMP" netns "$STALE_NS"
	ip netns exec "$STALE_NS" ip link set "$STALE_TMP" name eth1
	ip netns exec "$STALE_NS" ip link set lo up
	ip netns exec "$STALE_NS" ip link set eth1 up
	ip link set "$STALE_VETH" up
	echo "$BASHPID" > "$STALE_CG/cgroup.procs"
	"$HOSTILE" --linger &
	echo "$!" > "$STALE_PID_FILE"
	touch "$READY"
	wait
) >"$LOG_ROOT/stale-creator.log" 2>&1 &
STALE_CREATOR_PID=$!
PIDS+=("$STALE_CREATOR_PID")
wait_for_file "$READY"
kill -KILL "$STALE_CREATOR_PID" 2>/dev/null || true
wait "$STALE_CREATOR_PID" 2>/dev/null || true
"$STALE_REAPER" --instance-file "$INSTANCE_FILE" --state-dir "$STATE_DIR" --cgroup-root "$CGROUP_D" --run-root "$STALE_RUN_ROOT" | grep -qx 'STALE_REAP PASS' || fail "T0.7 durable state reaper failed"
[[ ! -e "$STALE_PID_FILE" ]] || fail "T0.7 stale hostile PID marker survived cleanup"
[[ ! -d "$STALE_CG" && ! -d "$STALE_RUN_DIR" ]] || fail "T0.7 stale cgroup or run directory remains"
! ip netns list | grep -Eq "^$STALE_NS[[:space:]]" || fail "T0.7 stale netns remains"
! ip link show dev "$STALE_VETH" >/dev/null 2>&1 || fail "T0.7 stale veth remains"
pass "T0.7 fresh durable-id/state-file sweep removed cgroup, netns, veth, run dir, and hostile process"

# T0.8 hostile probe: every isolation attempt must be DENIED except the
# deliberately accepted shared-rootfs read.
if ! ip link show dev "$HOST_IFACE" >/dev/null 2>&1; then
	ip link add "$HOST_IFACE" type dummy
	DEVS+=("$HOST_IFACE")
fi
HOST_FILE="$STATE_PARENT/accepted-host-file"
printf 'accepted-p0-host-read\n' > "$HOST_FILE"
chmod 0644 "$HOST_FILE"
HOSTILE_DIR="$RUN_PARENT/hostile-$ID"
install -d -o ioltool -g ioltool -m 0700 "$HOSTILE_DIR"
RUNDIRS+=("$HOSTILE_DIR")
IOLBOX_HOST_FILE="$HOST_FILE"
IOLBOX_HOSTILE_ORPHAN_PID_FILE="$HOSTILE_DIR/orphan.pid"
IOLBOX_HOSTILE_LINGER=1
HOSTILE_CG="$(make_cage "tool-hostile-$ID" 64M 64)"
CAGES+=("$HOSTILE_CG")
HOSTILE_LOG="$LOG_ROOT/hostile.log"
start_tool "$TOOL_NS" "$HOSTILE_CG" "$HOSTILE" "$HOSTILE_LOG"
HOSTILE_PID="$LAST_PID"
wait_for_line "$HOSTILE_LOG" '^P0_HOSTILE_PASS$'
for marker in CAP_REGAIN_DENIED HOST_IFACE_DENIED HOST_ROUTE_DENIED NET_ADMIN_DENIED CGROUP_ESCAPE_DENIED HOST_FILE_ACCEPTED_RISK; do
	grep -Eq "^$marker" "$HOSTILE_LOG" || fail "T0.8 missing hostile result: $marker"
done
! grep -Eq 'SUCCEEDED|P0_HOSTILE_FAIL' "$HOSTILE_LOG" || fail "T0.8 hostile isolation attempt succeeded"
kill -KILL "$HOSTILE_PID" 2>/dev/null || true
wait_for_file "$IOLBOX_HOSTILE_ORPHAN_PID_FILE"
ORPHAN_PID="$(cat "$IOLBOX_HOSTILE_ORPHAN_PID_FILE")"
kill_cgroup "$HOSTILE_CG"
wait_gone "$ORPHAN_PID"
pass "T0.8 hostile probe denied cap regain, host net view, NET_ADMIN, cgroup escape; accepted host read is recorded"

# T0.9: use the exact production bridge name and the stub's scapy endpoint.
test ! -e /sys/class/net/iolbr0 || fail "T0.9 refuses to overwrite an existing iolbr0"
ip link add iolbr0 type bridge
BRIDGE_CREATED=1
ip link set iolbr0 up
CAPTURE_NS="iolt$((ID + 2))"
CAPTURE_VETH="vtool$((ID + 2))"
CAPTURE_TMP="p0z$((ID + 2))"
PEER_NS="iolt$((ID + 3))"
PEER_VETH="vtool$((ID + 3))"
PEER_TMP="p0w$((ID + 3))"
for ns in "$CAPTURE_NS" "$PEER_NS"; do
	ip netns add "$ns"
	NETNS+=("$ns")
done
for tuple in "$CAPTURE_VETH $CAPTURE_TMP $CAPTURE_NS" "$PEER_VETH $PEER_TMP $PEER_NS"; do
	read -r veth tmp ns <<<"$tuple"
	ip link add "$veth" type veth peer name "$tmp"
	DEVS+=("$veth")
	ip link set "$tmp" netns "$ns"
	ip netns exec "$ns" ip link set "$tmp" name eth1
	ip netns exec "$ns" ip link set lo up
	ip netns exec "$ns" ip link set eth1 up
	ip link set "$veth" up
	ip link set "$veth" master iolbr0
done

T09_DIR="$RUN_PARENT/t09-$ID"
install -d -o ioltool -g ioltool -m 0700 "$T09_DIR"
RUNDIRS+=("$T09_DIR")
IOLBOX_TOOL_SOCK="$T09_DIR/gui.sock"
IOLBOX_TOOL_OPTIONS="$T09_DIR/options.json"
printf '%s\n' '{}' > "$IOLBOX_TOOL_OPTIONS"
chown ioltool:ioltool "$IOLBOX_TOOL_OPTIONS"
T09_CG="$(make_cage "tool-t09-$ID" 64M 64)"
CAGES+=("$T09_CG")
T09_LOG="$LOG_ROOT/t09-stub.log"
start_tool "$CAPTURE_NS" "$T09_CG" "$STUB" "$T09_LOG"
T09_PID="$LAST_PID"
wait_for_file "$IOLBOX_TOOL_SOCK"
BRIDGE_PCAP="$BUILD_ROOT/iolbr0.pcap"
PEER_PCAP="$BUILD_ROOT/peer.pcap"
tcpdump -nn -e -i iolbr0 -c 1 -w "$BRIDGE_PCAP" arp >"$LOG_ROOT/t09-bridge-tcpdump.log" 2>&1 &
BRIDGE_TCPDUMP_PID=$!
PIDS+=("$BRIDGE_TCPDUMP_PID")
ip netns exec "$PEER_NS" tcpdump -nn -e -i eth1 -c 1 -w "$PEER_PCAP" arp >"$LOG_ROOT/t09-peer-tcpdump.log" 2>&1 &
PEER_TCPDUMP_PID=$!
PIDS+=("$PEER_TCPDUMP_PID")
sleep 1
curl --fail --silent --show-error --unix-socket "$IOLBOX_TOOL_SOCK" http://localhost/send-arp | grep -qx 'sent' || fail "T0.9 stub scapy ARP sender failed"
for _ in $(seq 1 200); do
	[[ -s "$BRIDGE_PCAP" && -s "$PEER_PCAP" ]] && break
	sleep 0.05
done
set +e
wait "$BRIDGE_TCPDUMP_PID"
BRIDGE_RC=$?
wait "$PEER_TCPDUMP_PID"
PEER_RC=$?
set -e
[[ "$BRIDGE_RC" == 0 && "$PEER_RC" == 0 ]] || fail "T0.9 tcpdump did not capture one ARP frame"
BRIDGE_FRAMES="$(tcpdump -nn -r "$BRIDGE_PCAP" arp 2>/dev/null | wc -l)"
PEER_FRAMES="$(tcpdump -nn -r "$PEER_PCAP" arp 2>/dev/null | wc -l)"
(( BRIDGE_FRAMES >= 1 )) || fail "T0.9 iolbr0 capture pcap contains no ARP frame"
(( PEER_FRAMES >= 1 )) || fail "T0.9 bridge peer received no ARP frame"
pass "T0.9 peer RX count=$PEER_FRAMES and iolbr0 capture frame count=$BRIDGE_FRAMES"
kill_cgroup "$T09_CG"

# Restore <D> to its pre-probe shape, only after every level-3 leaf is gone.
# <D> is this process's own delegated cgroup (systemd owns the directory), so it
# is never rmdir'd -- we only undo the controllers this probe enabled and move
# back out of the level-2 leaf. Order matters for the same reason it did during
# bring-up: while +memory/+pids/+cpu are still in <D>/cgroup.subtree_control,
# <D> may not hold processes, so the disable must precede the migrate.
echo '-memory -pids -cpu' > "$CGROUP_D/cgroup.subtree_control" 2>/dev/null || true
CGROUP_D_SUBTREE_ENABLED=0
echo "$$" > "$CGROUP_D/cgroup.procs"
rmdir "$SUPERVISOR_LEAF" 2>/dev/null || true
SUPERVISOR_LEAF=""
CGROUP_D=""

cat <<'EOF'
P0 PASS: T0.1-T0.9 completed on this Linux target.
The PASS above is meaningful only for this target's current kernel/runtime.
EOF
