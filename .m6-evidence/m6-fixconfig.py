#!/usr/bin/env python3
# The image is an L2 (switch) IOL image (class="l2" per image.list), not L3 —
# physical ports are switchports by default, so "interface EthernetX/Y / ip
# address" (what the L3-style template config used) never takes effect
# ("unassigned" in show ip int brief). Fix live via an SVI (interface Vlan1)
# instead of reloading the nodes again.
import json, socket, sys, time

with open("/tmp/m6-lab-info.json") as f:
    info = json.load(f)


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


def configure(port, ip):
    sock = socket.create_connection(("127.0.0.1", port), timeout=10)
    sock.sendall(b"\r\n")
    drain(sock, 1)
    sock.sendall(b"enable\r")
    drain(sock, 1)
    sock.sendall(b"configure terminal\r")
    drain(sock, 1)
    sock.sendall(f"interface Vlan1\r".encode())
    drain(sock, 1)
    sock.sendall(f"ip address {ip} 255.255.255.0\r".encode())
    drain(sock, 1)
    sock.sendall(b"no shutdown\r")
    drain(sock, 1)
    sock.sendall(b"end\r")
    out = drain(sock, 2)
    sock.close()
    return out


r1_port = info["consolePorts"]["0"] if "0" in info["consolePorts"] else info["consolePorts"][0]
r2_port = info["consolePorts"]["1"] if "1" in info["consolePorts"] else info["consolePorts"][1]

print("R1 configure:")
print(configure(r1_port, "10.0.12.1"))
print("R2 configure:")
print(configure(r2_port, "10.0.12.2"))
