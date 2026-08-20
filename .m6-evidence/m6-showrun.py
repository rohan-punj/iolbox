#!/usr/bin/env python3
import json, socket, time

with open("/tmp/m6-lab-info.json") as f:
    info = json.load(f)
r1_port = info["consolePorts"]["0"] if "0" in info["consolePorts"] else info["consolePorts"][0]


def drain(sock, wait):
    buf = ""
    sock.settimeout(1.0)
    end = time.time() + wait
    while time.time() < end:
        try:
            chunk = sock.recv(4096)
            if chunk:
                buf += chunk.decode(errors="replace")
        except socket.timeout:
            pass
    return buf


sock = socket.create_connection(("127.0.0.1", r1_port), timeout=10)
sock.sendall(b"\r\n")
drain(sock, 1)
sock.sendall(b"show ip interface brief\r")
out = drain(sock, 5)
print("SHOW_IP_INT_BRIEF")
print(out)
sock.close()
