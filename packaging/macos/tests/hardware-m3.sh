#!/usr/bin/env bash
# hardware-m3.sh - Mac-host acceptance harness for the Darwin M3 contract.
#
# This is intentionally a Mac-side script. It does not create or mutate the
# irreplaceable iol22 machine; the default target is the disposable M3 VM.
# The deterministic product flow is the gated Go HTTP/WebSocket test, while
# this harness records the optional browser-tab smoke from /usr/bin/open.
set -euo pipefail

test_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$test_dir/../../.." && pwd)"
limactl_bin="${IOLBOX_LIMACTL:-/opt/homebrew/bin/limactl}"
machine="iolbox-m3-e2e"
gui_port="${IOLBOX_M3_GUI_PORT:-${IOLBOX_GUI_PORT:-4001}}"
export IOLBOX_GUI_PORT="$gui_port"

image_path=""
launcher_path="${IOLBOX_M3_LAUNCHER:-$repo_root/tools/iolab-launcher/iolbox-launcher}"
test_binary="${IOLBOX_M3_TEST_BINARY:-$repo_root/tools/iolab-launcher/iolbox-launcher-hardware.test}"
assets_dir="$repo_root/packaging/macos"
lab_yaml="${IOLBOX_M3_LAB_YAML:-$repo_root/supervisor/internal/server/seedlabs/seed-2-routers.yml}"
lab_json="${IOLBOX_M3_LAB_JSON:-$repo_root/labs/example-p0.lab.json}"
evidence_root="${IOLBOX_EVIDENCE_ROOT:-$repo_root/evidence}"
evidence_dir=""
probe_pid=""

usage() {
    cat <<'USAGE'
Usage: hardware-m3.sh [options]

  --machine NAME       disposable Lima machine (default: iolbox-m3-e2e)
  --image PATH         real x86_64 IOL image for the M1 phase and E2E test
  --launcher PATH      Darwin launcher binary
  --test-binary PATH   GOOS=darwin hardware test binary
  --assets-dir DIR     packaging/macos asset root
  --lab-yaml PATH      raw YAML fixture for lab.saveDoc
  --lab-json PATH      JSON fixture for lab.load
  --evidence-root DIR  evidence parent
USAGE
}

die() {
    printf 'FAIL: %s\n' "$*" >&2
    exit 1
}

require_file() {
    [ -f "$1" ] || die "file is missing: $1"
}

require_command() {
    command -v "$1" >/dev/null 2>&1 || die "required command is missing: $1"
}

new_evidence_dir() {
    local stamp
    stamp="$(date -u '+%Y%m%dT%H%M%SZ')-$$"
    evidence_dir="$evidence_root/m3-$stamp"
    mkdir -p "$evidence_dir"
    printf '%s\n' "$evidence_dir" > "$evidence_dir/00-evidence-directory.txt"
}

resource_gate() {
    local df_line available_kb
    df_line="$(df -Pk "$HOME" | tail -1)"
    available_kb="$(printf '%s\n' "$df_line" | awk '{print $4}')"
    case "$available_kb" in
        ''|*[!0-9]*) die "could not parse available disk space: $df_line" ;;
    esac
    [ "$available_kb" -ge 5242880 ] || die "less than 5 GiB is free on the Mac"
    printf '%s\n' "$df_line" > "$evidence_dir/df-home.txt"
    vm_stat > "$evidence_dir/vm-stat.txt"
    sw_vers > "$evidence_dir/sw-vers.txt"
    "$limactl_bin" list > "$evidence_dir/limactl-list-before.txt"
}

assert_safe_machine() {
    [ "$machine" != "iol22" ] || die 'iol22 is protected M0 evidence and may not be touched'
}

launcher_start() {
    "$launcher_path" start --assets-dir "$assets_dir" --machine "$machine" > "$evidence_dir/launcher-start.txt" 2>&1
}


optional_browser_tab_probe() {
    local gui_url queried
    gui_url="http://127.0.0.1:$gui_port/"
    queried=0
    if command -v osascript >/dev/null 2>&1; then
        if osascript -e 'tell application "Safari" to get URL of every tab of every window' > "$evidence_dir/browser-tabs.txt" 2>&1; then
            queried=1
            if grep -F "$gui_url" "$evidence_dir/browser-tabs.txt" >/dev/null 2>&1; then
                printf 'ok: Safari opened %s\n' "$gui_url"
                return 0
            fi
        fi
        if osascript -e 'tell application "Google Chrome" to get URL of every tab of every window' >> "$evidence_dir/browser-tabs.txt" 2>&1; then
            queried=1
            if grep -F "$gui_url" "$evidence_dir/browser-tabs.txt" >/dev/null 2>&1; then
                printf 'ok: Chrome opened %s\n' "$gui_url"
                return 0
            fi
        fi
    fi
    if [ "$queried" -eq 1 ]; then
        printf 'browser tab probe did not find %s\n' "$gui_url" >> "$evidence_dir/browser-tabs.txt"
        die "no Safari or Chrome tab matched $gui_url"
    fi
    printf 'browser tab probe unavailable; deterministic evidence remains browser-equivalent HTTP/WS\n' > "$evidence_dir/browser-tabs.txt"
}

synthetic_port_probe() {
    local guest_code probe_code ws_probe_code lsof_output port
    guest_code='import os,socket,select
ports=list(range(9000,9050))+list(range(5500,5530))
open("/tmp/iolbox-m3-port-probe.pid","w").write(str(os.getpid()))
ls=[]
for p in ports:
 s=socket.socket(); s.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1); s.bind(("127.0.0.1",p)); s.listen(8); ls.append((p,s))
while True:
 r,_,_=select.select([s for _,s in ls],[],[],1)
 for s in r:
  c,_=s.accept(); p=[p for p,x in ls if x is s][0]; c.sendall(str(p).encode()); c.close()'
    "$limactl_bin" shell "$machine" sudo python3 -c "$guest_code" > "$evidence_dir/port-probe-guest.txt" 2>&1 &
    probe_pid=$!
    trap 'kill "$probe_pid" >/dev/null 2>&1 || true; "$limactl_bin" shell "$machine" sudo sh -c "if [ -f /tmp/iolbox-m3-port-probe.pid ]; then kill $(cat /tmp/iolbox-m3-port-probe.pid) 2>/dev/null || true; rm -f /tmp/iolbox-m3-port-probe.pid; fi" >/dev/null 2>&1 || true' EXIT HUP INT TERM
    sleep 2
    if ! "$limactl_bin" shell "$machine" sudo sh -c 'test -s /tmp/iolbox-m3-port-probe.pid && kill -0 "$(cat /tmp/iolbox-m3-port-probe.pid)"' >/dev/null 2>&1; then
        die 'guest synthetic port probe exited before host dialing'
    fi
    if ! curl --silent --show-error --fail --connect-timeout 2 "http://127.0.0.1:$gui_port/" >/dev/null 2>&1; then
        die "real GUI HTTP listener is unavailable on 127.0.0.1:$gui_port"
    fi
    ws_probe_code='import base64,os,socket,urllib.request
port=int(os.environ.get("IOLBOX_M3_GUI_PORT","4001"))
origin="http://127.0.0.1:%d" % port
# The GUI bridge (wsbridge.go requireSession/sameOrigin) requires the same
# session cookie and Origin header a browser cookie jar would send, exactly
# like tools/iolab-launcher/wsclient.go dialControlWS. Fetch it the same way.
resp=urllib.request.urlopen(origin+"/", timeout=10)
cookie=None
for header in resp.headers.get_all("Set-Cookie") or []:
 if header.startswith("iolbox_session="):
  cookie=header.split(";",1)[0]
  break
if cookie is None: raise SystemExit("GET / carried no iolbox_session cookie")
s=socket.create_connection(("127.0.0.1",port),10)
key=base64.b64encode(os.urandom(16)).decode("ascii")
request=("GET /control HTTP/1.1\r\nHost: 127.0.0.1:%d\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: %s\r\nCookie: %s\r\nOrigin: %s\r\n\r\n" % (port,key,cookie,origin)).encode("ascii")
s.sendall(request)
status=s.recv(1024).split(b"\r\n",1)[0]
s.close()
if b" 101 " not in status: raise SystemExit("GUI WebSocket did not upgrade: %s" % status.decode("ascii","replace"))
print("real GUI HTTP and WebSocket listeners accepted")'
    IOLBOX_M3_GUI_PORT="$gui_port" python3 -c "$ws_probe_code" > "$evidence_dir/gui-websocket.txt"
    if ! "$limactl_bin" shell "$machine" sudo sh -c 'test -s /tmp/iolbox-m3-port-probe.pid && kill -0 "$(cat /tmp/iolbox-m3-port-probe.pid)"' >/dev/null 2>&1; then
        die 'guest synthetic port probe exited before forwarded-port dialing'
    fi
    probe_code='import socket
ports=list(range(9000,9050))+list(range(5500,5530))
for p in ports:
 s=socket.create_connection(("127.0.0.1",p),10); got=s.recv(32).decode(); s.close()
 if got != str(p): raise SystemExit("wrong echo on %d: %s" % (p,got))
print("probed %d forwarded console/capture ports" % len(ports))'
    python3 -c "$probe_code" > "$evidence_dir/port-probe-host.txt"
    lsof_output="$("/usr/sbin/lsof" -nP -iTCP -sTCP:LISTEN 2>/dev/null || true)"
    printf '%s\n' "$lsof_output" > "$evidence_dir/lsof-listeners.txt"
    for port in "$gui_port" $(seq 9000 9049) $(seq 5500 5529); do
        printf '%s\n' "$lsof_output" | grep -F "127.0.0.1:$port" >/dev/null 2>&1 || die "host listener $port is not loopback-only"
    done
    if curl --silent --show-error --connect-timeout 2 "http://127.0.0.1:4000/" >/dev/null 2>&1; then
        die 'guest control port 4000 is unexpectedly reachable on the Mac host'
    fi
    "$limactl_bin" shell "$machine" sudo sh -c 'if [ -f /tmp/iolbox-m3-port-probe.pid ]; then kill "$(cat /tmp/iolbox-m3-port-probe.pid)" 2>/dev/null || true; rm -f /tmp/iolbox-m3-port-probe.pid; fi' >/dev/null 2>&1 || true
    kill "$probe_pid" >/dev/null 2>&1 || true
    probe_pid=""
    trap - EXIT HUP INT TERM
}
run_m1_phase() {
    [ -n "$image_path" ] || die '--image is required for the M1 install-image-and-lab phase'
    require_file "$image_path"
    IOLBOX_LIMACTL="$limactl_bin" "$test_dir/hardware-m1.sh" \
        --phase install-image-and-lab --machine "$machine" --image "$image_path" \
        --evidence-root "$evidence_dir" > "$evidence_dir/m1-install.txt" 2>&1
}

run_browser_equivalent() {
    require_file "$test_binary"
    require_file "$image_path"
    require_file "$lab_yaml"
    require_file "$lab_json"
    local difficult_root difficult_images difficult_labs capture_path
    difficult_root="$evidence_dir/M3 data café"
    difficult_images="$difficult_root/images"
    difficult_labs="$difficult_root/labs"
    capture_path="$difficult_root/captures/M3 link 0.pcapng"
    mkdir -p "$difficult_images" "$difficult_labs" "$(dirname "$capture_path")"
    cp "$image_path" "$difficult_images/M3-image.bin"
    IOLBOX_M3_E2E=1 IOLBOX_M3_GUI_PORT="$gui_port" IOLBOX_M3_IMAGE="$difficult_images/M3-image.bin" \
        IOLBOX_M3_LAB_YAML="$lab_yaml" IOLBOX_M3_LAB_JSON="$lab_json" \
        IOLBOX_M3_CAPTURE_PATH="$capture_path" "$test_binary" \
        -test.run '^TestMacOSBrowserEquivalentE2E$' -test.v > "$evidence_dir/browser-equivalent.txt" 2>&1
    [ -s "$capture_path" ] || die 'browser-equivalent capture file is empty'
    if [ -x /Applications/Wireshark.app/Contents/MacOS/capinfos ]; then
        /Applications/Wireshark.app/Contents/MacOS/capinfos "$capture_path" > "$evidence_dir/capinfos.txt"
    elif command -v tshark >/dev/null 2>&1; then
        tshark -r "$capture_path" -q > "$evidence_dir/tshark.txt"
    else
        printf 'Wireshark tools unavailable; stdlib pcapng validator was authoritative\n' > "$evidence_dir/pcap-validator.txt"
    fi
}

sync_restart_check() {
    local default_root difficult_root difficult_images difficult_labs
    difficult_root="$evidence_dir/M3 data café"
    difficult_images="$difficult_root/images"
    difficult_labs="$difficult_root/labs"
    default_root="$HOME/Library/Application Support/iolbox"
    "$launcher_path" stop --assets-dir "$assets_dir" --machine "$machine" > "$evidence_dir/launcher-stop-default.txt" 2>&1
    [ -d "$default_root/images" ] || die "default images directory is missing: $default_root/images"
    [ -d "$default_root/labs" ] || die "default labs directory is missing: $default_root/labs"
    find "$default_root" -maxdepth 3 -type f -print | sort > "$evidence_dir/default-sync-files.txt"
    "$launcher_path" start --assets-dir "$assets_dir" --machine "$machine" --no-browser \
        --images-dir "$difficult_images" --labs-dir "$difficult_labs" > "$evidence_dir/launcher-start-difficult-path.txt" 2>&1
    [ -d "$difficult_images" ] && [ -d "$difficult_labs" ] || die 'difficult-path sync roots were not created'
    "$launcher_path" stop --assets-dir "$assets_dir" --machine "$machine" \
        --images-dir "$difficult_images" --labs-dir "$difficult_labs" > "$evidence_dir/launcher-stop-difficult-path.txt" 2>&1
}

main() {
    while [ "$#" -gt 0 ]; do
        case "$1" in
            --machine) [ "$#" -ge 2 ] || die '--machine needs a value'; machine="$2"; shift 2 ;;
            --image) [ "$#" -ge 2 ] || die '--image needs a value'; image_path="$2"; shift 2 ;;
            --launcher) [ "$#" -ge 2 ] || die '--launcher needs a value'; launcher_path="$2"; shift 2 ;;
            --test-binary) [ "$#" -ge 2 ] || die '--test-binary needs a value'; test_binary="$2"; shift 2 ;;
            --assets-dir) [ "$#" -ge 2 ] || die '--assets-dir needs a value'; assets_dir="$2"; shift 2 ;;
            --lab-yaml) [ "$#" -ge 2 ] || die '--lab-yaml needs a value'; lab_yaml="$2"; shift 2 ;;
            --lab-json) [ "$#" -ge 2 ] || die '--lab-json needs a value'; lab_json="$2"; shift 2 ;;
            --evidence-root) [ "$#" -ge 2 ] || die '--evidence-root needs a value'; evidence_root="$2"; shift 2 ;;
            -h|--help) usage; return 0 ;;
            *) usage >&2; return 1 ;;
        esac
    done
    assert_safe_machine
    require_command python3
    require_command curl
    require_command awk
    require_file "$launcher_path"
    require_file "$assets_dir/lima/profiles.env"
    [ -x "$limactl_bin" ] || die "limactl is not executable: $limactl_bin"
    new_evidence_dir
    resource_gate
    launcher_start
    optional_browser_tab_probe
    synthetic_port_probe
    run_m1_phase
    run_browser_equivalent
    sync_restart_check
    "$launcher_path" status --assets-dir "$assets_dir" --machine "$machine" > "$evidence_dir/status.txt" 2>&1 || true
    "$launcher_path" diagnose --assets-dir "$assets_dir" --machine "$machine" > "$evidence_dir/diagnose.txt" 2>&1 || true
    "$launcher_path" upgrade --assets-dir "$assets_dir" --machine "$machine" --no-browser > "$evidence_dir/upgrade.txt" 2>&1
    "$launcher_path" stop --assets-dir "$assets_dir" --machine "$machine" --no-sync > "$evidence_dir/launcher-stop-final.txt" 2>&1
    "$limactl_bin" list > "$evidence_dir/limactl-list-final.txt"
    printf 'PASS: M3 hardware evidence collected in %s\n' "$evidence_dir"
}

main "$@"
