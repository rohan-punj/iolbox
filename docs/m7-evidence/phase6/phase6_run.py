#!/usr/bin/env python3
"""Phase 6 A/B metrics collector. Runs ONE (arm, topology, run-index) trial
against a real launcher binary on this Mac and writes a JSON metrics file.

Usage:
  python3 phase6_run.py --arm rosetta-amd64|native-arm64 --topology 2node|4node
      --run N --launcher PATH --lima-home PATH [--tarball PATH] --outdir DIR
      --profile-flag rosetta-amd64|native-arm64 --vmname NAME
"""
import argparse, base64, json, os, re, select, socket, struct, subprocess, sys, time, signal

CONTROL_PORT = 4001
IMAGES_DIR = os.path.expanduser("~/Library/Application Support/iolbox/images")
IMAGE_FILENAME = "x86_64_crb_linux-adventerprisek9-ms.iol17.18.02.bin"
IMAGE_SRC = os.path.expanduser("~/iolbox-m0/" + IMAGE_FILENAME)

LAB_2NODE = os.path.expanduser("~/phase6/vpcs-iol.lab.json")
LAB_4NODE = os.path.expanduser("~/phase6/four-iol-ring.lab.json")


def now():
    return time.time()


def wait_for_frame(s, timeout):
    """Wait before starting a frame, then disable per-recv timeouts so an
    in-progress frame cannot be abandoned and desynchronize the stream."""
    readable, _, _ = select.select([s], [], [], timeout)
    if not readable:
        raise socket.timeout("timed out waiting for WebSocket frame")
    s.settimeout(None)


def recv_frame_bytes(s, length, deadline):
    """Read exactly length bytes, but never wait forever mid-frame."""
    buf = b""
    while len(buf) < length:
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise RuntimeError("timed out reading complete WebSocket frame")
        s.settimeout(remaining)
        try:
            chunk = s.recv(length - len(buf))
        except socket.timeout:
            raise RuntimeError("timed out reading complete WebSocket frame") from None
        if not chunk:
            raise EOFError("WebSocket closed mid-frame")
        buf += chunk
    return buf


def sh(cmd, timeout=60, env=None):
    e = dict(os.environ)
    if env:
        e.update(env)
    p = subprocess.run(cmd, shell=True, capture_output=True, text=True, timeout=timeout, env=e)
    return p.returncode, p.stdout, p.stderr


# ---------- WS control client ----------
class Control:
    def __init__(self, port=CONTROL_PORT):
        self.port = port
        origin = "http://127.0.0.1:%d" % port
        import urllib.request
        resp = urllib.request.urlopen(origin + "/", timeout=15)
        cookie = None
        for h in resp.headers.get_all("Set-Cookie") or []:
            if h.startswith("iolbox_session="):
                cookie = h.split(";", 1)[0]
                break
        self.cookie = cookie
        self.origin = origin
        self.s = socket.create_connection(("127.0.0.1", port), 15)
        key = base64.b64encode(os.urandom(16)).decode()
        req = ("GET /control HTTP/1.1\r\nHost: 127.0.0.1:%d\r\nUpgrade: websocket\r\n"
               "Connection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: %s\r\n"
               "Cookie: %s\r\nOrigin: %s\r\n\r\n" % (port, key, cookie, origin)).encode()
        self.s.sendall(req)
        buf = b""
        while b"\r\n\r\n" not in buf:
            buf += self.s.recv(1)

    def _send_text(self, payload):
        b = payload.encode()
        hdr = bytearray([0x81])
        l = len(b)
        mask = os.urandom(4)
        if l < 126:
            hdr.append(0x80 | l)
        elif l < 65536:
            hdr.append(0x80 | 126); hdr += struct.pack(">H", l)
        else:
            hdr.append(0x80 | 127); hdr += struct.pack(">Q", l)
        hdr += mask
        self.s.sendall(bytes(hdr) + bytes(c ^ mask[i % 4] for i, c in enumerate(b)))

    def _read_frame(self, ready_timeout=None):
        if ready_timeout is not None:
            wait_for_frame(self.s, ready_timeout)
        deadline = time.monotonic() + 30
        hdr = recv_frame_bytes(self.s, 2, deadline)
        opcode = hdr[0] & 0x0F
        length = hdr[1] & 0x7F
        if length == 126:
            length = struct.unpack(">H", recv_frame_bytes(self.s, 2, deadline))[0]
        elif length == 127:
            length = struct.unpack(">Q", recv_frame_bytes(self.s, 8, deadline))[0]
        payload = recv_frame_bytes(self.s, length, deadline)
        self.s.settimeout(None)
        return opcode, payload

    def request(self, op, args, req_id, timeout=90):
        self._send_text(json.dumps({"id": req_id, "op": op, "args": args}))
        deadline = now() + timeout
        while now() < deadline:
            try:
                opcode, payload = self._read_frame(
                    min(5, max(0, deadline - now())))
            except socket.timeout:
                continue
            if opcode == 1:
                try:
                    env = json.loads(payload)
                except Exception:
                    continue
                if env.get("id") == req_id:
                    return env
        raise TimeoutError("no response for " + op)

    def open_console(self, idx):
        s = socket.create_connection(("127.0.0.1", self.port), 15)
        key = base64.b64encode(os.urandom(16)).decode()
        path = "/console/%d" % idx
        req = ("GET %s HTTP/1.1\r\nHost: 127.0.0.1:%d\r\nUpgrade: websocket\r\n"
               "Connection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: %s\r\n"
               "Cookie: %s\r\nOrigin: %s\r\n\r\n" % (path, self.port, key, self.cookie, self.origin)).encode()
        s.sendall(req)
        buf = b""
        while b"\r\n\r\n" not in buf:
            buf += s.recv(1)
        return s


def cframe_send(s, data, opcode=0x2):
    b = data.encode() if isinstance(data, str) else data
    hdr = bytearray([0x80 | opcode])
    l = len(b)
    mask = os.urandom(4)
    if l < 126:
        hdr.append(0x80 | l)
    elif l < 65536:
        hdr.append(0x80 | 126); hdr += struct.pack(">H", l)
    else:
        hdr.append(0x80 | 127); hdr += struct.pack(">Q", l)
    hdr += mask
    s.sendall(bytes(hdr) + bytes(c ^ mask[i % 4] for i, c in enumerate(b)))


def cframe_read(s, ready_timeout=None):
    if ready_timeout is not None:
        wait_for_frame(s, ready_timeout)
    deadline = time.monotonic() + 30
    hdr = recv_frame_bytes(s, 2, deadline)
    opcode = hdr[0] & 0x0F
    length = hdr[1] & 0x7F
    if length == 126:
        length = struct.unpack(">H", recv_frame_bytes(s, 2, deadline))[0]
    elif length == 127:
        length = struct.unpack(">Q", recv_frame_bytes(s, 8, deadline))[0]
    payload = recv_frame_bytes(s, length, deadline)
    s.settimeout(None)
    return opcode, payload


def drain(s, seconds):
    out = b""
    end = now() + seconds
    while now() < end:
        try:
            opcode, payload = cframe_read(
                s, min(0.5, max(0, end - now())))
            if opcode in (1, 2):
                out += payload
        except socket.timeout:
            continue
    return out


# Same regexes proven working in macos_m4_runtime_darwin_test.go (m4IOSPingRE /
# m4PCPingRE / m4LatencyRE) -- reused verbatim rather than re-derived, since
# these are the exact, hardware-validated formats for Cisco IOS ("Success
# rate is X percent (R/S)") and Linux/VPCS ping ("S packets transmitted, R
# packets received, L% packet loss").
IOS_PING_RE = re.compile(rb"(?i)Success rate is ([0-9]+) percent \(([0-9]+)/([0-9]+)\)")
PC_PING_RE = re.compile(rb"(?i)([0-9]+) packets transmitted,\s*([0-9]+) packets received,\s*([0-9.]+)% packet loss")
LATENCY_RE = re.compile(rb"(?i)(?:min/avg/max(?:/mdev)?|round-trip min/avg/max)\s*=\s*([0-9.]+)/([0-9.]+)/([0-9.]+)")


# This VPCS build never prints an aggregate "X packets transmitted..."
# summary line at all (confirmed by raw console capture: individual
# per-packet reply/timeout lines only, no trailer) -- same thing
# macos_m4_runtime_darwin_test.go's own comment notes ("This vpcs build
# never prints an aggregate 'packets transmitted...' summary"), which is
# exactly why that reference implementation counts individual reply/timeout
# lines instead of relying on a summary. Reused verbatim here as the
# fallback when neither aggregate regex matches.
VPCS_REPLY_RE = re.compile(rb"(?i)bytes from \S+ icmp_seq=([0-9]+) ttl=[0-9]+ time=([0-9.]+)")
VPCS_TIMEOUT_RE = re.compile(rb"(?i)\S+ icmp_seq=([0-9]+) timeout")


def parse_ping(text):
    out = {"sent": None, "received": None, "loss_pct": None, "rtt_min": None, "rtt_avg": None, "rtt_max": None}
    m = IOS_PING_RE.search(text)
    if m:
        pct, received, sent = int(m.group(1)), int(m.group(2)), int(m.group(3))
        out["sent"] = sent; out["received"] = received
        out["loss_pct"] = round(100.0 * (sent - received) / sent, 2) if sent else None
    else:
        m2 = PC_PING_RE.search(text)
        if m2:
            out["sent"] = int(m2.group(1)); out["received"] = int(m2.group(2))
            out["loss_pct"] = float(m2.group(3))
        else:
            replies = VPCS_REPLY_RE.findall(text)
            timeouts = VPCS_TIMEOUT_RE.findall(text)
            if replies or timeouts:
                reply_seqs = set(int(s) for s, _ in replies)
                timeout_seqs = set(int(s) for s in timeouts)
                sent = len(reply_seqs | timeout_seqs)
                received = len(reply_seqs)
                out["sent"] = sent; out["received"] = received
                out["loss_pct"] = round(100.0 * (sent - received) / sent, 2) if sent else None
                times = [float(t) for _, t in replies]
                if times:
                    out["rtt_min"] = min(times); out["rtt_avg"] = round(sum(times) / len(times), 3); out["rtt_max"] = max(times)
    r = LATENCY_RE.search(text)
    if r:
        out["rtt_min"] = float(r.group(1)); out["rtt_avg"] = float(r.group(2)); out["rtt_max"] = float(r.group(3))
    return out


# NOT end-anchored to the whole buffer: IOS prints asynchronous syslog lines
# (LINEPROTO/PNP/PKI/etc.) that can keep arriving interleaved AFTER the
# prompt itself has appeared, permanently pushing the prompt away from the
# very end of the accumulated buffer. Instead this matches a whole LINE that
# looks like just a short hostname/token followed by '>' or '#' (router
# prompt) or 'VPCS> ' -- found anywhere in the buffer, via re.MULTILINE.
PROMPT_RE = re.compile(rb"(?m)^\r?[\w.-]{1,16}[>#] ?\r?$")


def wait_for_prompt(s, deadline_seconds):
    """Poll until the accumulated buffer ends with a router/VPCS-style
    prompt (e.g. 'R1>', 'R1#', 'VPCS> '), or the deadline elapses. Needed
    because a cold IOL node takes 15-20+s of boot-log chatter before it
    reaches a usable prompt -- a fixed short drain() sends commands into
    that chatter and loses them."""
    buf = b""
    end = now() + deadline_seconds
    while now() < end:
        try:
            opcode, payload = cframe_read(
                s, min(1, max(0, end - now())))
            if opcode in (1, 2):
                buf += payload
                if PROMPT_RE.search(buf):
                    return buf, True
        except socket.timeout:
            continue
    return buf, False


def console_ping(ctl, node_idx, is_router, target, count, timeout):
    s = ctl.open_console(node_idx)
    cframe_send(s, "\r\n")
    boot_buf, prompt_seen = wait_for_prompt(s, 90)
    if is_router:
        cframe_send(s, "enable\r\n"); time.sleep(1); drain(s, 1)
        cframe_send(s, "terminal length 0\r\n"); time.sleep(1); drain(s, 1)
        cframe_send(s, "ping %s repeat %d\r\n" % (target, count))
    else:
        cframe_send(s, "ping %s -c %d\r\n" % (target, count))
    out = drain(s, timeout)
    s.close()
    return parse_ping(out), boot_buf + out


def mac_vmstat():
    rc, out, err = sh("vm_stat")
    d = {}
    for line in out.splitlines():
        m = re.match(r"Pages (\w[\w ]*):\s+(\d+)\.", line)
        if m:
            d[m.group(1).strip()] = int(m.group(2))
    return d


def mac_top_cpu():
    rc, out, _ = sh("top -l 1 -n 0")
    m = re.search(r"CPU usage:\s*([\d.]+)% user,\s*([\d.]+)% sys,\s*([\d.]+)% idle", out)
    if m:
        return {"user": float(m.group(1)), "sys": float(m.group(2)), "idle": float(m.group(3))}
    return None


def mac_translator_ps(pattern):
    rc, out, _ = sh("ps aux")
    rows = []
    for line in out.splitlines():
        if re.search(pattern, line, re.I):
            parts = line.split(None, 10)
            if len(parts) >= 4:
                rows.append({"cmd": parts[10] if len(parts) > 10 else "", "cpu": parts[2], "mem": parts[3], "rss_kb": parts[5]})
    return rows


def guest_shell(vmname, lima_home, cmd, timeout=20):
    env = {"LIMA_HOME": lima_home}
    full = "/opt/homebrew/bin/limactl shell --tty=false %s -- %s" % (vmname, cmd)
    rc, out, err = sh(full, timeout=timeout, env=env)
    return rc, out, err


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--arm", required=True)
    ap.add_argument("--profile-flag", required=True)
    ap.add_argument("--topology", required=True, choices=["2node", "4node"])
    ap.add_argument("--run", type=int, required=True)
    ap.add_argument("--launcher", required=True)
    ap.add_argument("--lima-home", required=True)
    ap.add_argument("--vmname", required=True)
    ap.add_argument("--tarball", default=None)
    ap.add_argument("--outdir", required=True)
    ap.add_argument("--ping-count", type=int, default=50)
    args = ap.parse_args()

    os.makedirs(args.outdir, exist_ok=True)
    os.makedirs(args.lima_home, exist_ok=True)

    # Guarantee a genuinely clean starting point for every run. Observed
    # repeatedly on real hardware: reusing an already-provisioned VM across
    # runs (a warm limactl start/stop cycle) sometimes leaves node-launch
    # state (bound UDP ports / stale processes from a prior run, including a
    # prior run that crashed before its own teardown completed) that makes
    # the NEXT run's node.start hang deterministically at the same early
    # boot point. Deleting and recreating the VM every run trades "warm
    # restart realism" for measurement validity, and (a genuine plus) makes
    # every run's vm_boot_seconds a true cold/from-image boot -- arguably a
    # more faithful reading of the plan's "cold VM start" wording than a
    # warm restart would be. Also kills any leaked launcher process from a
    # prior crashed run before touching the VM.
    sh("pkill -f '%s' 2>/dev/null" % args.launcher.replace("'", "'\\''"))
    time.sleep(1)
    rc, out, _ = sh("/opt/homebrew/bin/limactl list", env={"LIMA_HOME": args.lima_home})
    if args.vmname in out:
        sh("/opt/homebrew/bin/limactl stop --tty=false %s" % args.vmname, timeout=60,
           env={"LIMA_HOME": args.lima_home})
        sh("/opt/homebrew/bin/limactl delete --tty=false -f %s" % args.vmname, timeout=60,
           env={"LIMA_HOME": args.lima_home})
    os.makedirs(os.path.dirname(IMAGES_DIR), exist_ok=True) if False else None
    os.makedirs(IMAGES_DIR, exist_ok=True)

    global LAST_METRICS, LAST_OUTDIR
    LAST_OUTDIR = args.outdir
    metrics = {
        "arm": args.arm, "topology": args.topology, "run": args.run,
        "vmname": args.vmname, "lima_home": args.lima_home,
        "started_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }
    LAST_METRICS = metrics

    # copy image into host sync folder (idempotent)
    if not os.path.exists(os.path.join(IMAGES_DIR, IMAGE_FILENAME)):
        sh("cp '%s' '%s/'" % (IMAGE_SRC, IMAGES_DIR))

    metrics["mac_pre_run"] = {"vm_stat": mac_vmstat(), "top": mac_top_cpu()}

    # launch
    launch_dir = os.path.dirname(args.launcher)
    logfile = os.path.join(args.outdir, "launcher.log")
    cmd = [args.launcher, "start", "-profile", args.profile_flag, "-no-browser"]
    if args.tarball:
        cmd += ["-tarball", args.tarball]
    env = dict(os.environ)
    env["LIMA_HOME"] = args.lima_home
    t0 = now()
    with open(logfile, "wb") as lf:
        proc = subprocess.Popen(cmd, cwd=launch_dir, stdout=lf, stderr=subprocess.STDOUT, env=env)

    # poll for GET / 200
    import urllib.request
    boot_deadline = now() + 900
    http_ok_at = None
    while now() < boot_deadline:
        try:
            r = urllib.request.urlopen("http://127.0.0.1:%d/" % CONTROL_PORT, timeout=3)
            if r.status == 200:
                http_ok_at = now()
                break
        except Exception:
            time.sleep(2)
    metrics["vm_boot_seconds"] = (http_ok_at - t0) if http_ok_at else None
    metrics["vm_boot_ok"] = http_ok_at is not None
    if not http_ok_at:
        metrics["error"] = "VM did not reach HTTP 200 within deadline"
        json.dump(metrics, open(os.path.join(args.outdir, "metrics.json"), "w"), indent=2)
        return 1

    time.sleep(3)
    ctl_box = [Control()]

    def robust_request(op, req_args, rid, timeout=60):
        """Bounded-retry control request: this frozen supervisor lineage has
        a documented (Phase 5 evidence) intermittent WS control-connection
        framing/hang issue -- reconnect once and replay on any failure,
        matching the mitigation Phase 5 applied for the same symptom."""
        try:
            return ctl_box[0].request(op, req_args, rid, timeout=timeout)
        except Exception as e1:
            try:
                ctl_box[0] = Control()
                return ctl_box[0].request(op, req_args, rid + "-retry", timeout=timeout)
            except Exception as e2:
                raise RuntimeError("robust_request %s failed twice: %r then %r" % (op, e1, e2))

    lab_id = None
    try:
        # register image
        guest_path = "/opt/iolbox/images/" + IMAGE_FILENAME
        reg_deadline = now() + 60
        image_id = None
        while now() < reg_deadline:
            try:
                r = robust_request("image.register", {"path": guest_path}, "reg1", timeout=30)
                if "result" in r and r["result"].get("id"):
                    image_id = r["result"]["id"]
                    break
            except Exception:
                pass
            time.sleep(2)
        metrics["image_register_ok"] = image_id is not None
        metrics["image_id"] = image_id

        lab_path = LAB_2NODE if args.topology == "2node" else LAB_4NODE
        lab = json.load(open(lab_path))
        for n in lab["nodes"]:
            if n.get("image", {}).get("id") == "REPLACE_WITH_IMAGE_ID":
                n["image"]["id"] = image_id

        r = robust_request("lab.load", {"lab": lab}, "load1", timeout=60)
        lab_id = r["result"]["labId"]
        metrics["lab_load_result_ok"] = "result" in r

        t2 = now()
        r = robust_request("lab.start", {"labId": lab_id, "nodes": None}, "start1", timeout=180)
        lab_start_resp_at = now()
        metrics["lab_start_response_ok"] = "result" in r
    except Exception as e:
        metrics["lab_setup_error"] = repr(e)
        metrics["lab_boot_ok"] = False
        metrics["lab_boot_seconds"] = None
        metrics["pings"] = []
        metrics["ended_at"] = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
        json.dump(metrics, open(os.path.join(args.outdir, "metrics.json"), "w"), indent=2)
        # still attempt a best-effort VM stop so we don't leak a running VM
        try:
            subprocess.run([args.launcher, "stop"], cwd=launch_dir, env=env, timeout=180)
        except Exception:
            pass
        return 1
    ctl = ctl_box[0]

    # wait for consoles usable: open each console and wait for an actual
    # router/VPCS prompt (not just any byte), matching what "usable" means
    # for the traffic phase that follows.
    n_nodes = 2 if args.topology == "2node" else 4
    # Wait for every node's console prompt CONCURRENTLY (not one-at-a-time --
    # sequential waiting wastes wall-clock and was observed to starve later
    # nodes of any real wait budget). Real cold IOL-via-Rosetta boot time on
    # this Mac was observed to vary widely (as little as ~20s, as much as
    # >240s) under ordinary host CPU contention from the owner's other apps
    # -- itself a real Phase 6-relevant characteristic, not a harness bug.
    usable_at = None
    per_node_deadline = 600
    node_errors = []
    socks = {}
    bufs = {i: b"" for i in range(n_nodes)}
    seen = {i: False for i in range(n_nodes)}
    try:
        for i in range(n_nodes):
            s = ctl.open_console(i)
            cframe_send(s, "\r\n")
            socks[i] = s
    except Exception as e:
        node_errors.append("console open failed: %r" % e)

    deadline = now() + per_node_deadline
    while now() < deadline and not all(seen.values()):
        for i, s in list(socks.items()):
            if seen[i]:
                continue
            try:
                opcode, payload = cframe_read(
                    s, min(0.3, max(0, deadline - now())))
                if opcode in (1, 2) and payload:
                    bufs[i] += payload
                    if PROMPT_RE.search(bufs[i]):
                        seen[i] = True
            except socket.timeout:
                continue
            except Exception as e:
                node_errors.append("node %d: read error %r" % (i, e))
                seen[i] = None  # stop polling this one, but don't count as success

    for i, s in socks.items():
        try:
            s.close()
        except Exception:
            pass
        if seen[i] is not True:
            node_errors.append("node %d: no prompt within %ds, tail=%r" % (i, per_node_deadline, bufs[i][-200:]))

    all_ok = all(seen.get(i) is True for i in range(n_nodes)) and len(socks) == n_nodes
    if all_ok:
        usable_at = now()
    metrics["lab_boot_node_errors"] = node_errors
    metrics["lab_boot_seconds"] = (usable_at - t2) if usable_at else None
    metrics["lab_boot_ok"] = usable_at is not None

    # idle sample before traffic
    time.sleep(3)
    metrics["idle_host_cpu"] = mac_top_cpu()
    metrics["idle_mac_vmstat"] = mac_vmstat()
    rc, out, _ = guest_shell(args.vmname, args.lima_home, "cat /proc/loadavg")
    metrics["idle_guest_loadavg"] = out.strip() if rc == 0 else None
    if args.arm == "rosetta-amd64":
        metrics["idle_translator_ps"] = mac_translator_ps(r"oahd|Rosetta|rosetta")
    else:
        rc, out, _ = guest_shell(args.vmname, args.lima_home, "ps aux | egrep 'qemu|binfmt' | grep -v egrep")
        metrics["idle_translator_ps_guest"] = out.strip() if rc == 0 else None

    # traffic
    pings = []
    raw_dir = os.path.join(args.outdir, "raw-console")
    os.makedirs(raw_dir, exist_ok=True)

    def do_ping(idx, is_router, target, label):
        p, raw = console_ping(ctl, idx, is_router, target, args.ping_count, args.ping_count + 30)
        with open(os.path.join(raw_dir, label.replace(">", "-") + ".txt"), "wb") as f:
            f.write(raw)
        pings.append({"pair": label, **p})

    if usable_at:
        if args.topology == "2node":
            # VPCS is documented (macos_m4_runtime_darwin_test.go: "vpcs is
            # the one node kind with no boot-time config path in the
            # supervisor") as never getting its lab.json "config.commands"
            # auto-applied -- the M4 reference test always types them via
            # console itself. Do the same here before the first ping, or
            # PC1 never actually has 192.168.1.10 and every ping in both
            # directions times out (observed and confirmed via raw console
            # capture on two separate native-arm64 runs before this fix).
            vpcs_cfg = ctl.open_console(1)
            cframe_send(vpcs_cfg, "\r\n")
            wait_for_prompt(vpcs_cfg, 30)
            cframe_send(vpcs_cfg, "ip 192.168.1.10 192.168.1.1 24\r\n")
            time.sleep(2)
            drain(vpcs_cfg, 2)
            vpcs_cfg.close()

            do_ping(1, False, "192.168.1.1", "PC1->R1")
            do_ping(0, True, "192.168.1.10", "R1->PC1")
        else:
            pairs = [(0, "10.0.12.2", "R1->R2"), (1, "10.0.23.2", "R2->R3"),
                     (2, "10.0.34.2", "R3->R4"), (3, "10.0.41.2", "R4->R1")]
            for idx, target, label in pairs:
                do_ping(idx, True, target, label)
    metrics["pings"] = pings

    # during-traffic sample (best-effort, sampled right after issuing last ping)
    metrics["during_host_cpu"] = mac_top_cpu()
    metrics["during_mac_vmstat"] = mac_vmstat()
    rc, out, _ = guest_shell(args.vmname, args.lima_home, "cat /proc/loadavg")
    metrics["during_guest_loadavg"] = out.strip() if rc == 0 else None

    # RSS per process, guest mem
    rc, out, _ = guest_shell(args.vmname, args.lima_home,
                              "ps -eo comm,rss | egrep -i 'iol|vpcs|supervisor' | grep -v egrep")
    metrics["guest_process_rss_kb"] = out.strip() if rc == 0 else None
    rc, out, _ = guest_shell(args.vmname, args.lima_home, "free -m")
    metrics["guest_free_m"] = out.strip() if rc == 0 else None

    # crash scan
    rc, out, _ = guest_shell(args.vmname, args.lima_home,
                              "dmesg 2>/dev/null | egrep -i 'segfault|sigill|sigsys|killed process' | tail -30")
    metrics["guest_dmesg_crash_grep"] = out.strip() if rc == 0 else None
    rc, out, _ = guest_shell(args.vmname, args.lima_home,
                              "journalctl -u iolbox --no-pager 2>/dev/null | egrep -i 'panic|sigill|sigsys|core dumped' | tail -30")
    metrics["guest_journal_crash_grep"] = out.strip() if rc == 0 else None

    # teardown -- reopen a fresh control connection first: the long
    # prompt-wait/traffic phase can leave the original one server-idle-closed.
    t4 = now()
    try:
        ctl2 = Control()
        ctl2.request("lab.stop", {"labId": lab_id, "nodes": None}, "stop1", timeout=60)
    except Exception as e:
        metrics["lab_stop_error"] = str(e)

    stop_cmd = [args.launcher, "stop"]
    with open(os.path.join(args.outdir, "stop.log"), "wb") as sf:
        subprocess.run(stop_cmd, cwd=launch_dir, stdout=sf, stderr=subprocess.STDOUT, env=env, timeout=180)
    # verify VM actually stopped
    stopped_at = None
    deadline = now() + 180
    while now() < deadline:
        rc, out, _ = sh("/opt/homebrew/bin/limactl list", env={"LIMA_HOME": args.lima_home})
        if args.vmname in out and "Stopped" in out.split(args.vmname, 1)[1].split("\n", 1)[0]:
            stopped_at = now()
            break
        time.sleep(2)
    metrics["teardown_seconds"] = (stopped_at - t4) if stopped_at else None
    metrics["teardown_ok"] = stopped_at is not None

    try:
        proc.terminate()
    except Exception:
        pass

    # stale resource check
    rc, out, _ = sh("/opt/homebrew/bin/limactl list", env={"LIMA_HOME": args.lima_home})
    metrics["post_teardown_lima_list"] = out.strip()
    rc, out, _ = sh("ps aux | egrep -i '%s' | grep -v egrep" % args.vmname)
    metrics["post_teardown_stray_procs"] = out.strip()

    metrics["mac_post_run"] = {"vm_stat": mac_vmstat(), "top": mac_top_cpu()}
    metrics["ended_at"] = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())

    json.dump(metrics, open(os.path.join(args.outdir, "metrics.json"), "w"), indent=2)
    print("WROTE", os.path.join(args.outdir, "metrics.json"))
    return 0


LAST_METRICS = None
LAST_OUTDIR = None

if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception as e:
        import traceback
        if LAST_METRICS is not None and LAST_OUTDIR is not None:
            LAST_METRICS["unhandled_exception"] = repr(e)
            LAST_METRICS["unhandled_traceback"] = traceback.format_exc()
            LAST_METRICS["ended_at"] = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
            try:
                json.dump(LAST_METRICS, open(os.path.join(LAST_OUTDIR, "metrics.json"), "w"), indent=2)
            except Exception:
                pass
        raise
