#!/usr/bin/env bash
# hardware-m1.sh - reproducible M1 evidence collector.
#
# This is deliberately a shell script rather than a test framework. It writes
# every control-plane response and guest check into a timestamped evidence
# directory, and any missing acceptance item makes the phase fail.
set -euo pipefail

test_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$test_dir/../../.." && pwd)"
limactl_bin="${IOLBOX_LIMACTL:-/opt/homebrew/bin/limactl}"
machine=""
image_path=""
phase="all"
evidence_root="${IOLBOX_EVIDENCE_ROOT:-$repo_root/evidence}"
evidence_dir=""
request_number=0
lab_id='m1-hardware-two-router'
lab_file=''
image_id=''
console_port=''
last_success=''
ping_percent=''

usage() {
    cat <<'USAGE'
Usage: hardware-m1.sh [options]

Run the reproducible Apple Silicon M1 acceptance evidence collector.

  --phase install-image-and-lab   install image, gate/restart service, save/load lab, ping
  --phase persistence-check       restart VM and verify durable service/image/lab evidence
  --machine NAME                  Lima machine (required for a real phase)
  --image PATH                    real x86_64 IOL image to copy/register
  --evidence-root DIR             evidence parent (default: ./evidence)
  --help                          show this help without probing the machine

The control plane is NDJSON on 127.0.0.1:4000 and request ids are strings.
Readiness is GET / with status < 500; there is no /api/health endpoint.
USAGE
}

die() {
    printf 'FAIL: %s\n' "$*" >&2
    exit 1
}

require() {
    if [ "$1" -eq 0 ]; then
        printf 'ok: %s\n' "$2"
    else
        die "$2"
    fi
}

ensure_command() {
    command -v "$1" >/dev/null 2>&1 || die "required command is missing: $1"
}

json_escape() {
    local value="$1"
    value=${value//\\/\\\\}
    value=${value//"/\\"}
    value=${value//$'\n'/\\n}
    value=${value//$'\r'/\\r}
    value=${value//$'\t'/\\t}
    printf '%s' "$value"
}

new_evidence_dir() {
    local stamp
    stamp="$(date -u '+%Y%m%dT%H%M%SZ')-$$"
    evidence_dir="$evidence_root/m1-$stamp"
    mkdir -p "$evidence_dir"
    printf '%s\n' "$evidence_dir" > "$evidence_dir/00-evidence-directory.txt"
}

guest_capture() {
    local label="$1"
    shift
    local output
    if output="$($limactl_bin shell "$machine" sudo "$@" 2>&1)"; then
        printf '%s\n' "$output" > "$evidence_dir/guest-$label.txt"
        printf '%s' "$output"
        return 0
    fi
    printf '%s\n' "$output" > "$evidence_dir/guest-$label.txt"
    printf '%s\n' "$output" >&2
    return 1
}

guest_run() {
    local label="$1"
    shift
    if guest_capture "$label" "$@" >/dev/null; then
        return 0
    fi
    return 1
}

guest_shell_capture() {
    local label="$1" command_text="$2" output
    if output="$($limactl_bin shell "$machine" sudo sh -c "$command_text" 2>&1)"; then
        printf '%s\n' "$output" > "$evidence_dir/guest-$label.txt"
        printf '%s' "$output"
        return 0
    fi
    printf '%s\n' "$output" > "$evidence_dir/guest-$label.txt"
    printf '%s\n' "$output" >&2
    return 1
}

# The control plane is a *streaming* NDJSON socket: the supervisor pushes
# unsolicited event frames (host.stats every second, node.state on change)
# for as long as the connection is open. That breaks the obvious
# `printf ... | nc` idiom in a way that is easy to misdiagnose:
#
#   - nc's -w is an IDLE timeout. The socket is never idle, so nc never
#     returns. Measured on hardware: `nc -w 5` against this port was still
#     running after 300 seconds.
#   - the first line back is almost always an event, not the reply, so any
#     read-one-line client mis-reads its own response.
#
# The reader below sends the request and closes as soon as the frame whose
# "id" matches arrives, ignoring event frames, with a hard wall-clock bound.
# Correlating by id is mandatory, not defensive.
CONTROL_READER_PY='
import json, socket, sys, time
req_id, op, args_json = sys.argv[1], sys.argv[2], sys.argv[3]
try:
    args = json.loads(args_json)
except ValueError:
    args = {}
deadline = time.time() + float(sys.argv[4] if len(sys.argv) > 4 else 120)
s = socket.create_connection(("127.0.0.1", 4000), timeout=10)
s.sendall((json.dumps({"id": req_id, "op": op, "args": args}) + "\n").encode())
buf = b""
while time.time() < deadline:
    s.settimeout(max(1.0, deadline - time.time()))
    try:
        chunk = s.recv(65536)
    except socket.timeout:
        break
    if not chunk:
        break
    buf += chunk
    while b"\n" in buf:
        line, buf = buf.split(b"\n", 1)
        if not line.strip():
            continue
        try:
            msg = json.loads(line)
        except ValueError:
            continue
        if msg.get("id") == req_id:
            # Compact separators are REQUIRED. json.dumps defaults to ", "/": ",
            # and the shell callers match the compact wire forms `"id":"..."`
            # and `"ok":true`. Re-serialising with default spacing silently
            # breaks every downstream match even though the frame is correct.
            print(json.dumps(msg, separators=(",", ":")))
            sys.exit(0)
sys.exit(3)
'

control_request() {
    local op="$1" args="$2" response wanted timeout="${3:-120}"
    request_number=$((request_number + 1))
    # The protocol contract requires a JSON string id, not a JSON number.
    wanted="m1-evidence-${request_number}"
    if ! response="$(printf '%s' "$CONTROL_READER_PY" | "$limactl_bin" shell "$machine" \
        sudo python3 - "$wanted" "$op" "$args" "$timeout" 2>&1)"; then
        printf '%s\n' "$response" > "$evidence_dir/control-${request_number}-${op}.ndjson"
        die "control request failed: $op"
    fi
    printf '%s\n' "$response" > "$evidence_dir/control-${request_number}-${op}.ndjson"
    response="$(printf '%s\n' "$response" | awk -v id="$wanted" 'index($0, "\"id\":\"" id "\"") { line = $0 } END { print line }')"
    [ -n "$response" ] || die "no correlated response for control request $op ($wanted)"
    printf '%s' "$response"
}

require_control_ok() {
    local response="$1" label="$2"
    case "$response" in
        *'"ok":true'*) printf 'ok: control %s\n' "$label" ;;
        *) printf '%s\n' "$response" >&2; die "control request was not ok: $label" ;;
    esac
}

# json_result_field <key> <response>
#
# Extract a key from INSIDE the response's "result" object. This exists because
# the envelope and the result both carry an "id", and the envelope's comes
# first:
#
#   {"id":"m1-evidence-1","ok":true,"result":{"id":"b858503827356c55",...}}
#     ^ correlation id                          ^ the image id you actually want
#
# A naive first-match therefore yields the REQUEST id. On hardware that
# produced a lab whose nodes referenced image "m1-evidence-1", and lab.start
# failed with `image_not_found` while every preceding step reported ok.
# Never use json_field for a key that also appears in the envelope.
json_result_field() {
    local key="$1" text="$2" tail
    tail="${text#*\"result\":}"
    [ "$tail" != "$text" ] || return 1
    json_field "$key" "$tail"
}

# json_number_field <key> <response>
#
# json_field only matches STRING values because it searches for `"key":"`.
# consolePort is a JSON number (`"consolePort":9000`), so json_field silently
# returns empty for it — which reads as "the server sent no console port"
# rather than "the extractor cannot see numbers".
json_number_field() {
    local key="$1" text="$2"
    printf '%s\n' "$text" | awk -v wanted="$key" '
        {
            marker = "\"" wanted "\":"
            start = index($0, marker)
            if (start) {
                value = substr($0, start + length(marker))
                sub(/[^0-9].*/, "", value)
                if (value != "") { print value; exit }
            }
        }
    '
}

json_field() {
    local key="$1" text="$2"
    printf '%s\n' "$text" | awk -v wanted="$key" '
        {
            marker = "\"" wanted "\":\""
            start = index($0, marker)
            if (start) {
                value = substr($0, start + length(marker))
                sub(/".*/, "", value)
                print value
                exit
            }
        }
    '
}

wait_guest_service() {
    local i state
    i=0
    while [ "$i" -lt 60 ]; do
        state="$(guest_capture "service-$i" systemctl is-active iolbox-supervisor.service || true)"
        if [ "$state" = active ]; then
            printf 'ok: supervisor active after structural-gate restart\n'
            return 0
        fi
        i=$((i + 1))
        sleep 1
    done
    return 1
}

check_structural_gate() {
    local gate
    gate="$(guest_capture structural-gate systemctl cat iolbox-supervisor.service || true)"
    case "$gate" in
        *'ExecStartPre='*'30-canary.sh --quiet'*)
            printf 'ok: systemd structural gate contains canary ExecStartPre\n'
            ;;
        *)
            die 'systemd structural gate is missing ExecStartPre=...30-canary.sh --quiet'
            ;;
    esac
}

check_http_ready() {
    local status i
    # This runs right after a systemd restart, so the listener may not be bound
    # yet: curl then exits 7 and reports status 000. Poll instead of sleeping and
    # hoping — earlier runs only passed because the supervisor happened to be up
    # already, which made this a latent flake rather than a fixed bug.
    # A 5xx is a real failure and fails immediately; only "not listening yet"
    # (000) is retried.
    i=0
    while [ "$i" -lt 30 ]; do
        status="$(guest_shell_capture http-root "curl --silent --show-error --output /dev/null --write-out '%{http_code}' http://127.0.0.1:4001/ || true")"
        case "$status" in
            ''|*[!0-9]*) status=000 ;;
        esac
        if [ "$status" != "000" ]; then
            break
        fi
        i=$((i + 1))
        sleep 2
    done
    case "$status" in ''|*[!0-9]*) die "GET / returned a non-numeric HTTP status: $status" ;; esac
    if [ "$status" = "000" ]; then
        die 'GET / never connected: the GUI port 4001 did not bind within 60s of the restart'
    fi
    if [ "$status" -lt 500 ]; then
        printf 'ok: GET / readiness status is %s (< 500, after %ss)\n' "$status" "$((i * 2))"
    else
        die "GET / readiness status is $status (expected < 500)"
    fi
}

install_image_and_gate() {
    local base gate_response list_response response escaped_yaml

    base="$(basename "$image_path")"
    "$limactl_bin" copy "$image_path" "$machine:/tmp/$base" > "$evidence_dir/host-copy-image.txt" 2>&1
    guest_run image-install mkdir -p /opt/iolbox/images
    guest_run image-move mv "/tmp/$base" "/opt/iolbox/images/$base"
    # 0755, NOT 0644. An IOL image is not a data file — the supervisor execs it
    # directly (through Rosetta here), so a non-executable image yields nodes
    # that never reach `running` while lab.load and lab.start both still report
    # ok. Observed exactly that on hardware: no IOL processes, no node-start
    # errors in the journal, and a silent wait for `running`.
    # docs/macos-handoff.md records the same requirement for the manual path.
    guest_run image-mode chmod 0755 "/opt/iolbox/images/$base"
    if guest_shell_capture image-present "test -s /opt/iolbox/images/$base" >/dev/null; then
        require 0 'real IOL image copied into the guest image directory'
    else
        require 1 'real IOL image copied into the guest image directory'
    fi
    # Assert the mode rather than trusting the chmod: this is the single
    # highest-cost silent failure in the whole harness.
    if guest_shell_capture image-executable "test -x /opt/iolbox/images/$base" >/dev/null; then
        require 0 'IOL image is executable (0755) so the supervisor can exec it'
    else
        require 1 'IOL image is executable (0755) so the supervisor can exec it'
    fi

    check_structural_gate
    guest_run daemon-reload systemctl daemon-reload
    guest_run supervisor-restart systemctl restart iolbox-supervisor.service
    require_guest_service=0
    if wait_guest_service; then require_guest_service=0; else require_guest_service=1; fi
    require "$require_guest_service" 'supervisor reaches active through the structural gate'
    guest_shell_capture supervisor-journal 'journalctl -u iolbox-supervisor.service -b --no-pager' || true
    check_http_ready

    response="$(control_request image.register "{\"path\":\"/opt/iolbox/images/$base\"}")"
    require_control_ok "$response" image.register
    image_id="$(json_result_field id "$response")"
    [ -n "$image_id" ] || die 'image.register returned no image id'
    printf '%s\n' "$image_id" > "$evidence_dir/image-id.txt"
    list_response="$(control_request image.list '{}')"
    require_control_ok "$list_response" image.list
    case "$list_response" in *"$base"*) printf 'ok: image.list finds the registered image\n' ;; *) die 'image.list did not return the registered image' ;; esac

    lab_file="$evidence_dir/m1-two-router.lab.yaml"
    escaped_yaml=''
    printf '%s\n' \
        'version: 1' \
        "id: $lab_id" \
        'name: M1 fixed two-router /30' \
        'description: Hardware acceptance topology' \
        'nodes:' \
        '  - id: 0' \
        '    kind: iol' \
        '    name: R1' \
        '    x: 240' \
        '    y: 200' \
        "    image:" \
        "      id: $image_id" \
        "      filename: $base" \
        '      class: l3' \
        '    ram: 1024' \
        '    ethernet: 1' \
        '    serial: 1' \
        '    startupConfig: |' \
        '      hostname R1' \
        '      interface Ethernet0/0' \
        '       ip address 10.0.0.1 255.255.255.252' \
        '       no shutdown' \
        '      end' \
        '  - id: 1' \
        '    kind: iol' \
        '    name: R2' \
        '    x: 560' \
        '    y: 200' \
        "    image:" \
        "      id: $image_id" \
        "      filename: $base" \
        '      class: l3' \
        '    ram: 1024' \
        '    ethernet: 1' \
        '    serial: 1' \
        '    startupConfig: |' \
        '      hostname R2' \
        '      interface Ethernet0/0' \
        '       ip address 10.0.0.2 255.255.255.252' \
        '       no shutdown' \
        '      end' \
        'links:' \
        '  - id: 0' \
        '    type: p2p' \
        '    endpoints:' \
        '      - node: 0' \
        '        interface: e0/0' \
        '      - node: 1' \
        '        interface: e0/0' > "$lab_file"
    escaped_yaml="$(json_escape "$(cat "$lab_file")")"
    response="$(control_request lab.saveDoc "{\"lab\":\"$escaped_yaml\"}")"
    require_control_ok "$response" lab.saveDoc
    list_response="$(control_request lab.listDocs '{}')"
    require_control_ok "$list_response" lab.listDocs
    case "$list_response" in *"$lab_id"*) printf 'ok: lab.listDocs returns the saved raw YAML lab\n' ;; *) die 'lab.listDocs did not return the saved lab' ;; esac
    # lab.saveDoc and lab.load are NOT symmetric, and this is the single
    # easiest protocol mistake to make here:
    #
    #   lab.saveDoc / lab.listDocs  -> the lab is a raw YAML *string*
    #   lab.load                    -> the lab is a structured JSON *object*
    #                                  (protocol.LabLoadArgs{ Lab lab.Lab })
    #
    # Passing the YAML string to lab.load fails with
    #   "cannot unmarshal string into Go struct field LabLoadArgs.lab of type lab.Lab"
    # which reads like a malformed lab rather than a wrong-shaped argument.
    # Field names below track supervisor/internal/lab/lab.go: Node{id,kind,name,
    # x,y,image{id,filename,class},ram,ethernet,serial,startupConfig} and
    # Link{id,type,endpoints[{node,interface}]}.
    lab_json="$(printf '{"lab":{"version":1,"id":"%s","name":"M1 fixed two-router /30","description":"Hardware acceptance topology","nodes":[{"id":0,"kind":"iol","name":"R1","x":240,"y":200,"image":{"id":"%s","filename":"%s","class":"l3"},"ram":1024,"ethernet":1,"serial":1,"startupConfig":"hostname R1\\ninterface Ethernet0/0\\n ip address 10.0.0.1 255.255.255.252\\n no shutdown\\nend\\n"},{"id":1,"kind":"iol","name":"R2","x":560,"y":200,"image":{"id":"%s","filename":"%s","class":"l3"},"ram":1024,"ethernet":1,"serial":1,"startupConfig":"hostname R2\\ninterface Ethernet0/0\\n ip address 10.0.0.2 255.255.255.252\\n no shutdown\\nend\\n"}],"links":[{"id":0,"type":"p2p","endpoints":[{"node":0,"interface":"e0/0"},{"node":1,"interface":"e0/0"}]}]}}' \
        "$lab_id" "$image_id" "$base" "$image_id" "$base")"
    printf '%s\n' "$lab_json" > "$evidence_dir/m1-two-router.lab.json"
    response="$(control_request lab.load "$lab_json")"
    require_control_ok "$response" lab.load
    # lab.load returns ok:true even when it cannot resolve a node's image, and
    # reports it only in result.warnings. That warning is a hard predictor of a
    # lab.start `image_not_found` two steps later, so treat it as fatal here
    # where the message still names the cause. Observed on hardware:
    #   warnings: ["node 0 references unregistered image m1-evidence-1"]
    # followed by lab.start failing every node while all prior steps said ok.
    case "$response" in
        *'unregistered image'*)
            printf '%s\n' "$response" >&2
            die 'lab.load warned about an unregistered image; the node image ids are wrong' ;;
    esac
    loaded_id="$(json_result_field labId "$response")"
    [ -n "$loaded_id" ] || die 'lab.load returned no labId'
    printf '%s\n' "$loaded_id" > "$evidence_dir/lab-id.txt"
    # Console ports are allocated by lab.LOAD, not lab.start:
    #   lab.load -> result.nodes[] = [{"id":0,"consolePort":9000},{"id":1,...}]
    #   lab.start -> result.started[] / result.failed[]  (no ports at all)
    # Capture them here, before $response is overwritten by lab.start. The
    # first consolePort in the array belongs to node 0 (R1).
    console_port="$(json_number_field consolePort "$response")"
    [ -n "$console_port" ] || die 'lab.load returned no R1 console port'
    printf '%s\n' "$console_port" > "$evidence_dir/r1-console-port.txt"

    response="$(control_request lab.start "{\"labId\":\"$loaded_id\",\"nodes\":[0,1]}")"
    require_control_ok "$response" lab.start
    # lab.start reports per-node outcomes in result.failed[] while still
    # returning ok:true overall. A non-empty failed[] means no node started.
    case "$response" in
        *'"failed":[{'*)
            printf '%s\n' "$response" >&2
            die 'lab.start reported per-node failures despite ok:true' ;;
    esac
    wait_nodes_running "$loaded_id"
    printf '%s\n' "$console_port" > "$evidence_dir/r1-console-port.txt"
    run_ping_and_record
}

wait_nodes_running() {
    local loaded_id="$1" i status running_count
    i=0
    while [ "$i" -lt 90 ]; do
        status="$(control_request status "{\"labId\":\"$loaded_id\"}")"
        running_count="$(printf '%s\n' "$status" | awk -F '"state":"running"' '{print NF - 1}')"
        printf '%s\n' "$status" > "$evidence_dir/status-running-$i.ndjson"
        if [ "$running_count" -ge 2 ]; then
            printf 'ok: both routers report running\n'
            return 0
        fi
        i=$((i + 1))
        sleep 2
    done
    die 'both routers did not reach running state'
}

# Console driver. `nc` is wrong here for the same reason it was wrong for the
# control plane, plus two console-specific hazards measured on hardware:
#
#   - A node reporting "running" is NOT ready. IOL keeps the process alive
#     while IOS boots (and even while IOS is wedged), so lab.start success says
#     nothing about the CLI. Full 17.18.02 boot took ~90s here. Firing the ping
#     immediately types it into a console that discards it.
#   - The console replays its ring buffer on connect, so the transcript starts
#     with the whole boot log. Any "Success rate" match must take the LAST
#     occurrence, never the first.
#
# So: wait for a real prompt, then send the ping, then read until the result.
CONSOLE_PING_PY='
import re, socket, sys, time
port = int(sys.argv[1]); boot_deadline = time.time() + float(sys.argv[2])
# The console listener is not necessarily serving the instant a node reports
# "running": connecting too early gets a socket that is accepted and then
# immediately closed, so the first write dies with BrokenPipeError. Retry the
# whole connect+wake until the peer actually stays up. Measured on hardware.
s = None
while time.time() < boot_deadline:
    try:
        c = socket.create_connection(("127.0.0.1", port), timeout=10)
        c.sendall(b"\r\n")
        s = c
        break
    except (ConnectionRefusedError, BrokenPipeError, ConnectionResetError, OSError):
        try:
            c.close()
        except Exception:
            pass
        time.sleep(2)
if s is None:
    sys.stderr.write("console port %d never accepted a stable connection\n" % port)
    sys.exit(6)
buf = ""
def pump(deadline):
    global buf
    s.settimeout(max(1.0, min(5.0, deadline - time.time())))
    try:
        data = s.recv(65536)
    except socket.timeout:
        return False
    if not data:
        return False
    buf += data.decode("utf-8", "replace")
    return True
# 1. wake the console and wait for an exec prompt (R1> or R1#)
prompt = re.compile(r"[\r\n][A-Za-z0-9_.-]+[>#]")
s.sendall(b"\r\n")
last_poke = time.time()
while time.time() < boot_deadline:
    pump(boot_deadline)
    if prompt.search(buf[-600:]):
        break
    if time.time() - last_poke > 10:
        s.sendall(b"\r\n"); last_poke = time.time()
else:
    sys.stderr.write("console never presented a prompt before the deadline\n")
    print(buf); sys.exit(4)
# 2. enter privileged EXEC. `ping <ip> repeat N` is an EXTENDED ping keyword and
#    is only valid at "R1#": at the user-EXEC "R1>" prompt IOS answers
#    "% Invalid input detected at \x27^\x27 marker." and never pings. IOL images have
#    no enable secret by default, so `enable` goes straight to #.
priv = re.compile(r"[\r\n][A-Za-z0-9_.-]+#")
if not priv.search(buf[-600:]):
    s.sendall(b"enable\r\n")
    enable_deadline = time.time() + 30
    while time.time() < enable_deadline:
        pump(enable_deadline)
        if priv.search(buf[-600:]):
            break
    else:
        sys.stderr.write("could not reach privileged EXEC (#) prompt\n")
        print(buf); sys.exit(5)
# 3. send the ping and read until IOS reports the result
s.sendall(b"ping 10.0.0.2 repeat 10\r\n")
ping_deadline = time.time() + 90
while time.time() < ping_deadline:
    pump(ping_deadline)
    if "Success rate" in buf:
        # let the trailing percentage/round-trip text arrive
        time.sleep(2); pump(time.time() + 3)
        break
print(buf)
sys.exit(0)
'

run_ping_and_record() {
    local console_output
    [ -n "$console_port" ] || die 'R1 console port is missing'
    if ! console_output="$(printf '%s' "$CONSOLE_PING_PY" | $limactl_bin shell "$machine" \
        sudo python3 - "$console_port" 180 2>&1)"; then
        printf '%s\n' "$console_output" > "$evidence_dir/r1-console.txt"
        die 'could not drive R1 console to a usable prompt'
    fi
    printf '%s\n' "$console_output" > "$evidence_dir/r1-console.txt"
    last_success="$(printf '%s\n' "$console_output" | awk '/Success rate/ { line = $0 } END { print line }')"
    ping_percent="$(printf '%s\n' "$last_success" | sed -nE 's/.*Success rate[^0-9]*([0-9]+)[[:space:]]*percent.*/\1/p')"
    case "$ping_percent" in ''|*[!0-9]*) die 'console output has no final numeric Success rate match' ;; esac
    printf '%s\n' "$last_success" > "$evidence_dir/last-success-rate.txt"
    printf 'console LAST Success rate: %s\n' "$last_success"
    if [ "$ping_percent" -ge 80 ]; then
        printf 'ok: last console Success rate is %s%% (>= 80%%)\n' "$ping_percent"
    else
        die "last console Success rate is ${ping_percent}% (expected >= 80%)"
    fi
}

check_persistence() {
    local service listener http host_id iourc cache saved journal docs_response
    # A real Lima restart is deliberately explicit here; this phase proves
    # that the structural gate and durable stores survive a guest reboot.
    "$limactl_bin" restart "$machine" > "$evidence_dir/host-restart.txt" 2>&1
    service="$(guest_capture persistence-service systemctl is-active iolbox-supervisor.service)"
    [ "$service" = active ] || die 'supervisor is not active after persistence restart'
    printf 'ok: supervisor remains active after VM restart\n'

    listener="$(guest_shell_capture persistence-listener "ss -ltn")"
    case "$listener" in *':4000 '*) printf 'ok: control listener remains bound on 127.0.0.1:4000\n' ;; *) die 'control listener is not bound on port 4000' ;; esac
    http="$(guest_shell_capture persistence-http "curl --silent --show-error --output /dev/null --write-out '%{http_code}' http://127.0.0.1:4001/")"
    case "$http" in ''|*[!0-9]*) die "persistence GET / returned non-numeric status: $http" ;; esac
    [ "$http" -lt 500 ] || die "persistence GET / returned $http (expected < 500)"
    printf 'ok: persistence GET / status is %s (< 500)\n' "$http"

    host_id="$(guest_shell_capture persistence-hostid 'hostid')"
    [ -n "$host_id" ] || die 'hostid evidence is empty'
    printf 'ok: hostid is present\n'
    iourc="$(guest_shell_capture persistence-iourc 'find /opt/iolbox /root -type f -name iourc -size +0c -print -quit 2>/dev/null')"
    [ -n "$iourc" ] || die 'iourc evidence is missing'
    printf 'ok: iourc is present (%s)\n' "$iourc"
    cache="$(guest_shell_capture persistence-image-cache 'find /opt/iolbox/images -maxdepth 1 -type f -name .image-cache.json -size +0c -print -quit 2>/dev/null')"
    [ -n "$cache" ] || die 'image cache evidence is missing'
    printf 'ok: image cache is present (%s)\n' "$cache"
    saved="$(guest_shell_capture persistence-saved-lab "find /opt/iolbox/labs -maxdepth 1 -type f -print 2>/dev/null | sort")"
    case "$saved" in *"$lab_id"*) printf 'ok: saved two-router lab survives restart\n' ;; *) die 'saved two-router lab is missing after restart' ;; esac
    journal="$(guest_shell_capture persistence-journal 'journalctl -u iolbox-supervisor.service -b --no-pager' || true)"
    case "$journal" in
        *'30-canary'*|*'structural gate'*|*'ExecStartPre'*) printf 'ok: journal contains structural-gate/canary evidence\n' ;;
        *) die 'journal has no evidence that the structural gate ran' ;;
    esac
    docs_response="$(control_request lab.listDocs '{}')"
    require_control_ok "$docs_response" lab.listDocs-persistence
    case "$docs_response" in *"$lab_id"*) printf 'ok: control plane still lists the saved raw YAML lab\n' ;; *) die 'control plane lost the saved lab after restart' ;; esac
}

main() {
    local uname_s uname_m base
    while [ "$#" -gt 0 ]; do
        case "$1" in
            --phase)
                [ "$#" -ge 2 ] || { usage >&2; exit 1; }
                phase="$2"
                shift 2
                ;;
            --machine)
                [ "$#" -ge 2 ] || { usage >&2; exit 1; }
                machine="$2"
                shift 2
                ;;
            --image)
                [ "$#" -ge 2 ] || { usage >&2; exit 1; }
                image_path="$2"
                shift 2
                ;;
            --evidence-root)
                [ "$#" -ge 2 ] || { usage >&2; exit 1; }
                evidence_root="$2"
                shift 2
                ;;
            -h|--help)
                usage
                return 0
                ;;
            *)
                usage >&2
                return 1
                ;;
        esac
    done

    case "$phase" in
        all|install-image-and-lab|persistence-check) ;;
        *) die "unknown phase: $phase" ;;
    esac
    [ -n "$machine" ] || die '--machine is required for a real phase'
    case "$phase" in
        all|install-image-and-lab)
            [ -n "$image_path" ] || die '--image is required for install-image-and-lab'
            [ -f "$image_path" ] || die "IOL image is missing: $image_path"
            ;;
    esac
    ensure_command "$limactl_bin"
    ensure_command nc
    ensure_command awk
    ensure_command sed
    new_evidence_dir
    uname_s="$(uname -s)"
    uname_m="$(uname -m)"
    printf 'host=%s/%s\n' "$uname_s" "$uname_m" > "$evidence_dir/host-facts.txt"
    printf 'machine=%s\nphase=%s\nimage=%s\n' "$machine" "$phase" "${image_path:-}" > "$evidence_dir/run-metadata.txt"

    case "$phase" in
        all|install-image-and-lab)
            install_image_and_gate
            ;;
    esac
    case "$phase" in
        all|persistence-check)
            check_persistence
            ;;
    esac
    printf 'PASS: M1 hardware acceptance evidence collected in %s\n' "$evidence_dir"
}

main "$@"
