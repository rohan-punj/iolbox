#!/usr/bin/env python3
"""Telnet console driver for M6's IOL lab qualification. Carries forward
known gotchas from M3/M5: IOS doesn't print an unsolicited prompt (must
"wake" with a keystroke and re-poke while waiting), telnet echo can arrive
split across reads (accumulate into a buffer), and `--More--` pagination
must be disabled before any command that can produce >1 page of output."""
import json
import socket
import sys
import time

with open("/tmp/m6-lab-info.json") as f:
    info = json.load(f)

CONSOLE_HOST = "127.0.0.1"


def wait_for_prompt(sock, buf, timeout=90, poke_every=5):
    deadline = time.time() + timeout
    last_poke = 0.0
    while time.time() < deadline:
        now = time.time()
        if now - last_poke > poke_every:
            try:
                sock.sendall(b"\r\n")
            except OSError:
                pass
            last_poke = now
        sock.settimeout(1.0)
        try:
            chunk = sock.recv(4096)
            if chunk:
                buf[0] += chunk.decode(errors="replace")
        except socket.timeout:
            continue
        lines = [l for l in buf[0].splitlines() if l.strip()]
        if lines and (lines[-1].rstrip().endswith("#") or lines[-1].rstrip().endswith(">")):
            return True
    return False


def send_cmd(sock, buf, cmd, wait=3):
    sock.sendall((cmd + "\r").encode())
    time.sleep(wait)
    sock.settimeout(1.0)
    try:
        while True:
            chunk = sock.recv(4096)
            if not chunk:
                break
            buf[0] += chunk.decode(errors="replace")
    except socket.timeout:
        pass


def drive_node(name, port):
    print(f"=== {name} (port {port}) ===", flush=True)
    sock = socket.create_connection((CONSOLE_HOST, port), timeout=10)
    buf = [""]
    ok = wait_for_prompt(sock, buf, timeout=240)
    print(f"{name}: prompt reached = {ok}", flush=True)
    if not ok:
        print(f"{name}: LAST 2000 CHARS:\n{buf[0][-2000:]}", flush=True)
        sock.close()
        return False, buf[0]
    send_cmd(sock, buf, "terminal length 0", wait=2)
    send_cmd(sock, buf, "show version | include Software|image", wait=3)
    sock.close()
    return True, buf[0]


def ping_from(name, port, target):
    print(f"=== {name} ping {target} ===", flush=True)
    sock = socket.create_connection((CONSOLE_HOST, port), timeout=10)
    buf = [""]
    wait_for_prompt(sock, buf, timeout=30)
    buf[0] = ""  # reset, keep only the ping's own output
    send_cmd(sock, buf, f"ping {target} repeat 20", wait=15)
    sock.close()
    return buf[0]


r1_port = info["consolePorts"]["0"] if "0" in info["consolePorts"] else info["consolePorts"][0]
r2_port = info["consolePorts"]["1"] if "1" in info["consolePorts"] else info["consolePorts"][1]

ok1, out1 = drive_node("R1", r1_port)
ok2, out2 = drive_node("R2", r2_port)

if ok1 and ok2:
    ping_out = ping_from("R1", r1_port, "10.0.12.2")
    print("PING_OUTPUT_START")
    print(ping_out)
    print("PING_OUTPUT_END")
    with open("/tmp/m6-ping-result.txt", "w") as f:
        f.write(ping_out)
else:
    print("FATAL: one or both nodes never reached a console prompt", file=sys.stderr)
    sys.exit(1)
