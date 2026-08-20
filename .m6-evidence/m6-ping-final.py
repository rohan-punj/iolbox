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
drain(sock, 2)
sock.sendall(b"terminal length 0\r")
drain(sock, 2)
sock.sendall(b"ping 10.0.12.2\r")
out = drain(sock, 20)
print("PING_OUTPUT_START")
print(out)
print("PING_OUTPUT_END")
sock.close()
