#!/usr/bin/env python3
import json, socket, sys, time

with open("/tmp/m6-lab-info.json") as f:
    info = json.load(f)
port = info["consolePorts"]["0"] if "0" in info["consolePorts"] else info["consolePorts"][0]


def wait_for_prompt(sock, buf, timeout, poke_every=5):
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


sock = socket.create_connection(("127.0.0.1", port), timeout=10)
buf = [""]
ok = wait_for_prompt(sock, buf, timeout=300)
print(f"R1: prompt reached = {ok}", flush=True)
print("LAST 3000 CHARS:")
print(buf[0][-3000:])
sock.close()
